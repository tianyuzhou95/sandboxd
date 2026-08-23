// Copyright (c) 2026 Ant Group Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package firecracker

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	checkpointImageName               = "checkpoint.img"
	firecrackerCheckpointStateName    = "vmstate"
	firecrackerCheckpointMemoryName   = "memory"
	firecrackerCheckpointOverlayName  = "overlay.ext4"
	firecrackerCheckpointMaxComponent = int64(16 << 40)
)

type firecrackerCheckpointFiles struct {
	State   string
	Memory  string
	Overlay string
}

func createFirecrackerCheckpointArchive(
	ctx context.Context,
	imagePath string,
	compress bool,
	files firecrackerCheckpointFiles,
) (retErr error) {
	image, err := os.OpenFile(imagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create Firecracker checkpoint archive: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			retErr = errors.Join(retErr, os.Remove(imagePath))
		}
	}()

	var archiveOutput io.Writer = image
	var compressor *gzip.Writer
	if compress {
		compressor, err = gzip.NewWriterLevel(image, gzip.BestSpeed)
		if err != nil {
			return errors.Join(err, image.Close())
		}
		archiveOutput = compressor
	}
	archive := tar.NewWriter(archiveOutput)
	var writeErr error
	for _, component := range []struct {
		name string
		path string
	}{
		{name: firecrackerCheckpointStateName, path: files.State},
		{name: firecrackerCheckpointMemoryName, path: files.Memory},
		{name: firecrackerCheckpointOverlayName, path: files.Overlay},
	} {
		writeErr = writeFirecrackerCheckpointFile(
			ctx,
			archive,
			component.name,
			component.path,
		)
		if writeErr != nil {
			break
		}
	}
	closeErr := archive.Close()
	if compressor != nil {
		closeErr = errors.Join(closeErr, compressor.Close())
	}
	closeErr = errors.Join(closeErr, image.Sync(), image.Close())
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	complete = true
	return nil
}

func writeFirecrackerCheckpointFile(
	ctx context.Context,
	archive *tar.Writer,
	name,
	path string,
) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Firecracker checkpoint component %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > firecrackerCheckpointMaxComponent {
		return fmt.Errorf(
			"Firecracker checkpoint component %s is not a bounded regular file",
			name,
		)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Firecracker checkpoint component %s: %w", name, err)
	}
	defer file.Close()
	if err := archive.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0600,
		Size: info.Size(),
	}); err != nil {
		return err
	}
	written, err := copyFirecrackerCheckpoint(ctx, archive, file)
	if err != nil {
		return fmt.Errorf("write Firecracker checkpoint component %s: %w", name, err)
	}
	if written != info.Size() {
		return fmt.Errorf("Firecracker checkpoint component %s changed while reading", name)
	}
	return nil
}

func extractFirecrackerCheckpointArchive(
	ctx context.Context,
	imagePath string,
	files firecrackerCheckpointFiles,
) (retErr error) {
	imageInfo, err := os.Lstat(imagePath)
	if err != nil || !imageInfo.Mode().IsRegular() ||
		imageInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Firecracker checkpoint archive is not a regular file")
	}
	image, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("open Firecracker checkpoint archive: %w", err)
	}
	defer image.Close()

	buffered := bufio.NewReader(image)
	var archiveInput io.Reader = buffered
	var compressor *gzip.Reader
	magic, _ := buffered.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		compressor, err = gzip.NewReader(buffered)
		if err != nil {
			return fmt.Errorf("open Firecracker checkpoint compression: %w", err)
		}
		defer compressor.Close()
		archiveInput = compressor
	}

	outputs := map[string]string{
		firecrackerCheckpointStateName:   files.State,
		firecrackerCheckpointMemoryName:  files.Memory,
		firecrackerCheckpointOverlayName: files.Overlay,
	}
	created := make([]string, 0, len(outputs))
	defer func() {
		if retErr == nil {
			return
		}
		for _, path := range created {
			retErr = errors.Join(retErr, os.Remove(path))
		}
	}()
	seen := make(map[string]bool, len(outputs))
	archive := tar.NewReader(archiveInput)
	for {
		header, nextErr := archive.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read Firecracker checkpoint archive: %w", nextErr)
		}
		path, ok := outputs[header.Name]
		if !ok || seen[header.Name] || header.Typeflag != tar.TypeReg ||
			header.Size <= 0 || header.Size > firecrackerCheckpointMaxComponent {
			return fmt.Errorf("invalid Firecracker checkpoint entry %q", header.Name)
		}
		seen[header.Name] = true
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("create Firecracker checkpoint component %s: %w", header.Name, err)
		}
		created = append(created, path)
		written, copyErr := copyFirecrackerCheckpoint(ctx, output, archive)
		closeErr := errors.Join(output.Sync(), output.Close())
		if err := errors.Join(copyErr, closeErr); err != nil {
			return fmt.Errorf("extract Firecracker checkpoint component %s: %w", header.Name, err)
		}
		if written != header.Size {
			return fmt.Errorf("Firecracker checkpoint component %s has truncated content", header.Name)
		}
	}
	for name := range outputs {
		if !seen[name] {
			return fmt.Errorf("Firecracker checkpoint component %s is missing", name)
		}
	}
	return nil
}

func copyFirecrackerCheckpoint(
	ctx context.Context,
	destination io.Writer,
	source io.Reader,
) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			outputCount, writeErr := destination.Write(buffer[:count])
			written += int64(outputCount)
			if writeErr != nil {
				return written, writeErr
			}
			if outputCount != count {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}
