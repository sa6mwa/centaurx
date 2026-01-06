package podman

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/pslog"
)

// Builder implements shipohoy.Builder using the Podman API.
type Builder struct {
	addresses []string
}

// NewBuilder constructs a Podman builder with fallback socket addresses.
func NewBuilder(cfg Config) *Builder {
	return &Builder{addresses: candidateAddresses(cfg.Address)}
}

// Build builds an image using Podman.
func (b *Builder) Build(ctx context.Context, spec shipohoy.BuildSpec) (shipohoy.BuildResult, error) {
	return b.build(ctx, spec, nil)
}

// BuildWithEvents builds an image and streams progress events.
func (b *Builder) BuildWithEvents(ctx context.Context, spec shipohoy.BuildSpec, events chan<- shipohoy.BuildEvent) (shipohoy.BuildResult, error) {
	return b.build(ctx, spec, events)
}

func (b *Builder) build(ctx context.Context, spec shipohoy.BuildSpec, events chan<- shipohoy.BuildEvent) (shipohoy.BuildResult, error) {
	log := pslog.Ctx(ctx).With("backend", "podman")
	if len(spec.Tags) == 0 {
		log.Warn("podman build rejected", "reason", "missing tags")
		return shipohoy.BuildResult{}, errors.New("build tags are required")
	}
	contextDir := spec.ContextDir
	if contextDir == "" {
		log.Warn("podman build rejected", "reason", "missing context")
		return shipohoy.BuildResult{}, errors.New("build context is required")
	}
	if len(spec.ContainerfileData) > 0 {
		dockerfilePath := spec.ContainerfilePath
		if dockerfilePath == "" {
			dockerfilePath = filepath.Join(contextDir, "Containerfile")
		}
		if err := os.WriteFile(dockerfilePath, spec.ContainerfileData, 0o600); err != nil {
			return shipohoy.BuildResult{}, err
		}
	}
	dockerfilePath := spec.ContainerfilePath
	if dockerfilePath == "" {
		dockerfilePath = filepath.Join(contextDir, "Containerfile")
	}
	relDockerfile, err := filepath.Rel(contextDir, dockerfilePath)
	if err != nil || strings.HasPrefix(relDockerfile, "..") {
		log.Warn("podman build rejected", "reason", "dockerfile outside context", "path", dockerfilePath)
		return shipohoy.BuildResult{}, fmt.Errorf("dockerfile must be within context: %s", dockerfilePath)
	}

	client, err := b.dial(ctx)
	if err != nil {
		log.Warn("podman build failed", "err", err)
		return shipohoy.BuildResult{}, err
	}

	ctx, cancel := withTimeout(ctx, spec.Timeout)
	defer cancel()
	log.Info("podman build start", "tags", len(spec.Tags))

	tarStream, err := buildContextTar(contextDir)
	if err != nil {
		log.Warn("podman build failed", "err", err)
		return shipohoy.BuildResult{}, err
	}
	defer func() { _ = tarStream.Close() }()

	query := url.Values{}
	query.Set("dockerfile", relDockerfile)
	for _, tag := range spec.Tags {
		query.Add("t", tag)
	}
	if len(spec.BuildArgs) > 0 {
		args, err := json.Marshal(spec.BuildArgs)
		if err != nil {
			log.Warn("podman build failed", "err", err)
			return shipohoy.BuildResult{}, err
		}
		query.Set("buildargs", string(args))
	}

	res, err := client.do(ctx, "POST", "/build", query, tarStream, "application/x-tar")
	if err != nil {
		log.Warn("podman build failed", "err", err)
		return shipohoy.BuildResult{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		log.Warn("podman build failed", "status", res.StatusCode)
		return shipohoy.BuildResult{}, readAPIError(res)
	}

	if err := decodeBuildStream(ctx, res.Body, events); err != nil {
		log.Warn("podman build failed", "err", err)
		return shipohoy.BuildResult{}, err
	}

	if spec.OutputPath != "" {
		if err := exportImage(ctx, client, spec.Tags[0], spec.OutputPath); err != nil {
			log.Warn("podman build failed", "err", err)
			return shipohoy.BuildResult{}, err
		}
	}
	log.Info("podman build ok", "tags", len(spec.Tags))
	return shipohoy.BuildResult{ImageNames: spec.Tags}, nil
}

func (b *Builder) dial(ctx context.Context) (*client, error) {
	var lastErr error
	for _, addr := range b.addresses {
		cl, err := newClient(addr)
		if err != nil {
			lastErr = err
			continue
		}
		if err := cl.ping(ctx); err != nil {
			lastErr = err
			continue
		}
		return cl, nil
	}
	if lastErr == nil {
		lastErr = errors.New("podman address not configured")
	}
	return nil, lastErr
}

func buildContextTar(root string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == root {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = filepath.ToSlash(rel)
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				_, err = io.Copy(tw, file)
				_ = file.Close()
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			err = tw.Close()
		}
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()
	return pr, nil
}

func decodeBuildStream(ctx context.Context, body io.Reader, events chan<- shipohoy.BuildEvent) error {
	const maxLine = 1024 * 1024
	scanner := bufio.NewScanner(body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLine)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var resp buildResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			sendBuildEvent(ctx, events, shipohoy.BuildEvent{
				Kind:      shipohoy.BuildEventLog,
				Name:      "podman.build",
				Message:   line,
				Timestamp: time.Now(),
			})
			continue
		}
		if resp.Error != "" || resp.ErrorDetail.Message != "" {
			msg := resp.Error
			if msg == "" {
				msg = resp.ErrorDetail.Message
			}
			return errors.New(msg)
		}
		if resp.Stream != "" {
			sendBuildEvent(ctx, events, shipohoy.BuildEvent{
				Kind:      shipohoy.BuildEventLog,
				Name:      "podman.build",
				Message:   strings.TrimSpace(resp.Stream),
				Timestamp: time.Now(),
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func exportImage(ctx context.Context, client *client, image, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	pslog.Ctx(ctx).Info("build.export.start", "path", outputPath, "backend", "podman")
	tryExport := func(query url.Values) (*os.File, error) {
		res, err := client.do(ctx, "GET", "/images/"+escapeImagePath(image)+"/get", query, nil, "")
		if err != nil {
			return nil, err
		}
		if res.StatusCode >= 300 {
			err = readAPIError(res)
			_ = res.Body.Close()
			return nil, err
		}
		file, err := os.Create(outputPath)
		if err != nil {
			_ = res.Body.Close()
			return nil, err
		}
		_, err = io.Copy(file, res.Body)
		_ = res.Body.Close()
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	query := url.Values{}
	query.Set("format", "oci-archive")
	file, err := tryExport(query)
	if err != nil {
		file, err = tryExport(nil)
	}
	if err == nil {
		_ = file.Close()
	}
	if err == nil {
		pslog.Ctx(ctx).Info("build.export.complete", "path", outputPath, "backend", "podman")
	}
	return err
}

func sendBuildEvent(ctx context.Context, events chan<- shipohoy.BuildEvent, event shipohoy.BuildEvent) {
	if events == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case events <- event:
	default:
	}
}
