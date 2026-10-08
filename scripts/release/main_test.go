package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

func TestArchivesPreservePayloadAndExecutableMode(t *testing.T) {
	files := []payload{{"LICENSE", []byte("license\n"), 0644}, {"treeport", []byte("binary\x00bytes"), 0755}}
	for _, format := range []string{"tar", "zip"} {
		t.Run(format, func(t *testing.T) {
			write := writeTar
			if format == "zip" {
				write = writeZIP
			}
			var first, second bytes.Buffer
			if err := write(&first, files); err != nil {
				t.Fatal(err)
			}
			if err := write(&second, files); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first.Bytes(), second.Bytes()) {
				t.Fatal("archive bytes change between identical builds")
			}
			if format == "zip" {
				archive, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
				if err != nil {
					t.Fatal(err)
				}
				if len(archive.File) != len(files) {
					t.Fatal("wrong member count")
				}
				for i, file := range archive.File {
					r, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(r)
					r.Close()
					if err != nil || file.Name != files[i].name || int64(file.Mode().Perm()) != files[i].mode || !bytes.Equal(data, files[i].data) {
						t.Fatalf("ZIP member does not match: %s (%v)", file.Name, err)
					}
				}
				return
			}
			gz, err := gzip.NewReader(&first)
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			archive := tar.NewReader(gz)
			for _, file := range files {
				header, err := archive.Next()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(archive)
				if err != nil || header.Name != file.name || header.Mode != file.mode || !bytes.Equal(data, file.data) {
					t.Fatalf("tar member does not match: %s (%v)", header.Name, err)
				}
			}
			if _, err := archive.Next(); err != io.EOF {
				t.Fatalf("unexpected extra archive member: %v", err)
			}
		})
	}
}
