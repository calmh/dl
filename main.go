package main

import (
	"archive/tar"
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"bytes"
	"compress/gzip"
	"io/ioutil"
	"path"
	"strings"
)

var verbose = false

func main() {
	destination := flag.String("destination", "", "Destination to unpack into")
	strip := flag.Int("strip", 0, "Strip path components from archive")
	flag.BoolVar(&verbose, "v", verbose, "Verbose output")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Println("URL as only parameter")
		os.Exit(2)
	}

	dst := *destination
	if dst == "" {
		base := filepath.Base(flag.Arg(0))
		for ext := filepath.Ext(base); ext != ""; ext = filepath.Ext(base) {
			base = base[:len(base)-len(ext)]
		}

		dst = base
	}
	tmp := dst + ".tmp"

	if verbose {
		fmt.Println("Destination is", dst)
		fmt.Println("Downloading...")
	}

	if err := download(flag.Arg(0), tmp, *strip); err != nil {
		fmt.Println("Download:", err)
		os.Exit(1)
	}

	if err := os.Rename(tmp, dst); err != nil {
		if verbose {
			fmt.Println("Move destination into place...")
		}

		fmt.Println("Rename temporary:", err)
		os.Exit(1)
	}
}

func download(url, destination string, strip int) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	if path.Ext(url) == ".zip" {
		bs, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return unzip(bs, destination, strip)
	}

	return untar(resp.Body, destination, strip)
}

// --- https://github.com/mholt/archiver/ ---

func unzip(data []byte, destination string, strip int) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, zf := range r.File {
		if err := unzipFile(zf, destination, strip); err != nil {
			return err
		}
	}

	return nil
}

func unzipFile(zf *zip.File, destination string, strip int) error {
	name := zf.Name
	if strip > 0 {
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) <= strip {
			return nil
		}
		name = strings.Join(parts[strip:], "/")
	}

	if name == "" {
		return nil
	}

	if verbose {
		fmt.Println(" -", name)
	}

	if strings.HasSuffix(name, "/") {
		return mkdir(filepath.Join(destination, name))
	}

	rc, err := zf.Open()
	if err != nil {
		return fmt.Errorf("%s: open compressed file: %v", name, err)
	}
	defer rc.Close()

	return writeNewFile(filepath.Join(destination, name), rc, zf.FileInfo().Mode())
}

// untar un-tarballs the contents of tr into destination.
func untar(r io.Reader, destination string, strip int) error {
	gr, err := gzip.NewReader(r)
	if err == nil {
		r = gr
	}
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		if err := untarFile(tr, header, destination, strip); err != nil {
			return err
		}
	}
	return nil
}

// untarFile untars a single file from tr with header header into destination.
func untarFile(tr *tar.Reader, header *tar.Header, destination string, strip int) error {
	name := header.Name
	if strip > 0 {
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) <= strip {
			return nil
		}
		name = strings.Join(parts[strip:], "/")
	}

	if name == "" {
		return nil
	}

	if verbose {
		fmt.Println(" -", name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		return mkdir(filepath.Join(destination, name))
	case tar.TypeReg, tar.TypeRegA, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return writeNewFile(filepath.Join(destination, name), tr, header.FileInfo().Mode())
	case tar.TypeSymlink:
		return writeNewSymbolicLink(filepath.Join(destination, name), header.Linkname)
	case tar.TypeLink:
		return writeNewHardLink(filepath.Join(destination, name), filepath.Join(destination, header.Linkname))
	default:
		return fmt.Errorf("%s: unknown type flag: %c", name, header.Typeflag)
	}
}

func writeNewFile(fpath string, in io.Reader, fm os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	out, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("%s: creating new file: %v", fpath, err)
	}
	defer out.Close()

	err = out.Chmod(fm)
	if err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("%s: changing file mode: %v", fpath, err)
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("%s: writing file: %v", fpath, err)
	}
	return nil
}

func writeNewSymbolicLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	err = os.Symlink(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making symbolic link for: %v", fpath, err)
	}

	return nil
}

func writeNewHardLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	err = os.Link(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making hard link for: %v", fpath, err)
	}

	return nil
}

func mkdir(dirPath string) error {
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory: %v", dirPath, err)
	}
	return nil
}
