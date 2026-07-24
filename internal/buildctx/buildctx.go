// Package buildctx packages a docker build context as a tar.gz stream for
// shipping to a build host. Exclusions follow .dockerignore semantics via
// docker's own pattern matcher, so a context built on the host sees the same
// files a local `docker build` would send.
package buildctx

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// WriteTar streams dir as a gzipped tarball to w, honoring .dockerignore in
// dir. The Dockerfile and .dockerignore themselves are always included,
// matching docker's context upload rules.
func WriteTar(w io.Writer, dir, dockerfile string) error {
	patterns, err := readIgnore(dir)
	if err != nil {
		return err
	}
	matcher, err := patternmatcher.New(patterns)
	if err != nil {
		return fmt.Errorf("invalid .dockerignore pattern: %w", err)
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if rel != dockerfile && rel != ".dockerignore" {
			matched, err := matcher.MatchesOrParentMatches(rel)
			if err != nil {
				return err
			}
			if matched {
				// A matched directory can be skipped wholesale unless a !
				// exception could re-include something beneath it.
				if d.IsDir() && !matcher.Exclusions() {
					return filepath.SkipDir
				}
				if !d.IsDir() {
					return nil
				}
			}
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = rel
		if d.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uid, hdr.Gid = 0, 0
		hdr.Uname, hdr.Gname = "", ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func readIgnore(dir string) ([]string, error) {
	f, err := os.Open(filepath.Join(dir, ".dockerignore"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	patterns, err := ignorefile.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read .dockerignore: %w", err)
	}
	return patterns, nil
}
