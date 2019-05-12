package main

import (
	"crypto/md5"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/zosv2/modules"
)

const (
	defaultStorage = "zdb://hub.grid.tf:9900"
	defaultRoot    = "/var/modules/flist"
)

type flistModule struct {
	// root directory where all
	// the working file of the module will be located
	root string

	// underneath are the path for each
	// sub folder used by the flist module
	flist      string
	cache      string
	mountpoint string
	pid        string
	log        string
}

// New creates a new flistModule
func New(root string) modules.Flister {
	if root == "" {
		root = defaultRoot
	}

	// prepare directory layout for the module
	for _, path := range []string{"flist", "cache", "mountpoint", "pid", "log"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0770); err != nil {
			panic(err)
		}
	}

	return &flistModule{
		root:       root,
		flist:      filepath.Join(root, "flist"),
		cache:      filepath.Join(root, "cache"),
		mountpoint: filepath.Join(root, "mountpoint"),
		pid:        filepath.Join(root, "pid"),
		log:        filepath.Join(root, "log"),
	}
}

// Mount implements the Flister.Mount interface
func (f *flistModule) Mount(url, storage string) (string, error) {
	sublog := log.With().Str("url", url).Str("storage", storage).Logger()
	sublog.Info().Msg("request to mount flist")

	if storage == "" {
		storage = defaultStorage
	}

	flistPath, err := f.downloadFlist(url)
	if err != nil {
		sublog.Err(err).Msg("fail to download flist")
		return "", err
	}

	rnd, err := random()
	if err != nil {
		sublog.Error().Err(err).Msg("fail to generate random id for the mount")
		return "", err
	}
	mountpoint := filepath.Join(f.mountpoint, rnd)
	if err := os.MkdirAll(mountpoint, 0770); err != nil {
		return "", err
	}
	pidPath := filepath.Join(f.pid, rnd) + ".pid"
	logPath := filepath.Join(f.log, rnd) + ".log"

	args := []string{
		"-backend", filepath.Join(f.root, "backend", rnd),
		"-cache", f.cache,
		"-meta", flistPath,
		"-storage-url", storage,
		"-daemon",
		"-pid", pidPath,
		"-logfile", logPath,
		mountpoint,
	}
	sublog.Info().Strs("args", args).Msg("starting 0-fs daemon")
	cmd := exec.Command("g8ufs", args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		sublog.Err(err).Str("out", string(out)).Msg("fail to start 0-fs daemon")
		return "", err
	}

	//FIXME: find a better way to know when 0-fs is read
	// if I don't sleep here, the pid file can already be created while the
	// filesystem might be not ready yet
	time.Sleep(time.Second)
	if err := waitPidFile(time.Second*2, pidPath, true); err != nil {
		return "", err
	}

	return mountpoint, nil
}

// Umount implements the Flister.Umount interface
func (f *flistModule) Umount(path string) error {
	log.Info().Str("path", path).Msg("request unmount flist")

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("specified path is not a directory")
	}

	if !strings.HasPrefix(path, f.root) {
		return fmt.Errorf("trying to unmount a directory outside of the flist module boundraries")
	}

	if err := syscall.Unmount(path, syscall.MNT_DETACH); err != nil {
		log.Error().Err(err).Str("path", path).Msg("fail to umount flist")
	}
	_, name := filepath.Split(path)
	pidPath := filepath.Join(f.pid, name) + ".pid"
	if err := waitPidFile(time.Second*2, pidPath, false); err != nil {
		log.Error().Err(err).Str("path", path).Msg("0-fs daemon did not stopped properly")
		return err
	}

	// clean up working dirs
	logPath := filepath.Join(f.log, name) + ".log"
	backend := filepath.Join(f.root, "backend", name)
	_ = os.RemoveAll(logPath)
	_ = os.RemoveAll(backend)
	_ = os.RemoveAll(path)

	return nil
}

// downloadFlist downloads an flits from a URL
// if the flist location also provide and md5 hash of the flist
// this function will use it to avoid downloading an flist that is
// already present locally
func (f *flistModule) downloadFlist(url string) (string, error) {
	// first check if the md5 of the flist is available
	md5URL := url + ".md5"
	resp, err := http.Get(md5URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		hash, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		flistPath := filepath.Join(f.flist, strings.TrimSpace(string(hash)))
		_, err = os.Stat(flistPath)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil {
			log.Info().Str("url", url).Msg("flist already in cache")
			// flist is already present locally, just return its path
			return flistPath, nil
		}
	}

	log.Info().Str("url", url).Msg("flist not in cache, downloading")
	// we don't have the flist locally yet, let's download it
	resp, err = http.Get(url)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("fail to download flist: %v", resp.Status)
	}

	return f.saveFlist(resp.Body)
}

// saveFlist save the flist contained in r
// it save the flist by its md5 hash
// to avoid loading the full flist in memory to compute the hash
// it uses a MultiWriter to write the flist in a temporary file and fill up
// the md5 hash then it rename the file to the hash
func (f *flistModule) saveFlist(r io.Reader) (string, error) {
	tmp, err := ioutil.TempFile(f.flist, "*_flist_temp")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	h := md5.New()
	mr := io.MultiWriter(tmp, h)
	if _, err := io.Copy(mr, r); err != nil {
		return "", err
	}

	hash := fmt.Sprintf("%x", h.Sum(nil))
	path := filepath.Join(f.flist, hash)
	if err := os.MkdirAll(filepath.Dir(path), 0770); err != nil {
		return "", err
	}

	if os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}

	return path, nil
}

func random() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return fmt.Sprintf("%x", b), err
}

// waitPidFile wait for a file pointed by path to be created or deleted
// for at most timeout duration
// is exists is true, it waits for the file to exists
// else it waits for the file to be deleted
func waitPidFile(timeout time.Duration, path string, exists bool) error {
	delay := time.Millisecond * 100
	cTimeout := time.After(timeout)
	cDone := make(chan struct{})

	go func() {
		for {
			_, err := os.Stat(path)
			if exists {
				if err != nil {
					time.Sleep(delay)
					continue
				} else {
					break
				}
			} else {
				if err == nil {
					time.Sleep(delay)
				} else {
					break
				}
			}
		}
		cDone <- struct{}{}
	}()

	select {
	case <-cTimeout:
		return fmt.Errorf("timeout wait for pid file")
	case <-cDone:
		return nil
	}
}

var _ modules.Flister = (*flistModule)(nil)
