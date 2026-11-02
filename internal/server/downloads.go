package server

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// Games from a link: the person pastes an address, the server fetches the
// file into LIBRARY_DIR/.uploads (resuming after a dropped connection) and
// files it like an upload. Share pages of Google Drive, Dropbox, OneDrive and
// archive.org items are turned into the file behind them. Addresses on the
// local network are only fetched for admins.

const maxDownloads = 2

func (s *Server) wakeDownloads() {
	select {
	case s.dlWake <- struct{}{}:
	default:
	}
}

// downloader starts queued downloads, at most maxDownloads at a time.
func (s *Server) downloader(ctx context.Context) {
	s.store.RequeueInterrupted(ctx)
	for {
		ids, _ := s.store.QueuedDownloads(ctx)
		for _, id := range ids {
			s.dlMu.Lock()
			if len(s.dlRunning) >= maxDownloads || s.dlRunning[id] != nil {
				s.dlMu.Unlock()
				continue
			}
			dctx, cancel := context.WithCancel(ctx)
			s.dlRunning[id] = cancel
			s.dlMu.Unlock()
			go func(id string) {
				defer func() {
					s.dlMu.Lock()
					delete(s.dlRunning, id)
					s.dlMu.Unlock()
					s.wakeDownloads()
				}()
				s.runDownload(dctx, id)
			}(id)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.dlWake:
		case <-time.After(time.Minute):
		}
	}
}

func strp(v string) *string { return &v }
func i64p(v int64) *int64   { return &v }

func (s *Server) failDownload(ctx context.Context, id string, err error) {
	msg := err.Error()
	_ = s.store.UpdateDownload(context.WithoutCancel(ctx), id, store.DownloadPatch{Status: strp("failed"), Error: &msg})
}

// runDownload fetches one link (up to three tries, resuming) and files it.
func (s *Server) runDownload(ctx context.Context, id string) {
	d, err := s.store.GetDownload(ctx, 0, id)
	if err != nil || d.Status != "queued" {
		return
	}
	u, err := s.store.GetUser(ctx, d.UserID)
	if err != nil {
		return
	}
	_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Status: strp("downloading"), Error: strp("")})
	src := d.SourceURL
	if src == "" {
		if src, err = s.resolveLink(ctx, u, d.URL); err != nil {
			s.failDownload(ctx, id, err)
			return
		}
		_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{SourceURL: &src})
	}
	var last error
	for try := 0; try < 3; try++ {
		if try > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(try*try) * 5 * time.Second):
			}
		}
		last = s.fetchTo(ctx, u, id, src)
		if last == nil || errors.Is(last, errPermanent) || ctx.Err() != nil {
			break
		}
		s.log.Info("download interrupted, resuming", "id", id, "err", last)
	}
	if ctx.Err() != nil {
		return // canceled
	}
	if last != nil {
		s.failDownload(ctx, id, last)
		return
	}
	s.fileDownload(ctx, id)
}

var errPermanent = errors.New("permanent")

type permErr struct{ msg string }

func (e permErr) Error() string { return e.msg }
func (e permErr) Unwrap() error { return errPermanent }
func (e permErr) Is(t error) bool {
	return t == errPermanent
}

func permanent(format string, a ...any) error { return permErr{fmt.Sprintf(format, a...)} }

// fetchTo downloads src into the download's .part file, continuing where it stopped.
func (s *Server) fetchTo(ctx context.Context, u *store.User, id, src string) error {
	d, err := s.store.GetDownload(ctx, 0, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.lib.Temp, 0o755); err != nil {
		return err
	}
	part := s.partPath(id)
	var have int64
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return permanent("this is not a link that can be fetched")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := s.linkClient(u).Do(req)
	if err != nil {
		var pe permErr
		if errors.As(err, &pe) {
			return pe
		}
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && have > 0 && d.SizeBytes == have:
		return nil // all of it is here already
	case resp.StatusCode == http.StatusPartialContent:
	case resp.StatusCode == http.StatusOK:
		have = 0 // the server sends it all again
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return permanent("the link points to nothing (HTTP %d)", resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return permanent("the site wants a sign-in for this file (HTTP %d) — use a public link", resp.StatusCode)
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("the site answered HTTP %d", resp.StatusCode)
	default:
		return permanent("the site answered HTTP %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	name := fileNameFrom(resp)
	if strings.HasPrefix(ct, "text/html") && !hasGameExt(name) {
		return permanent("the link opens a web page, not a file — copy the download link itself")
	}
	total := resp.ContentLength
	if resp.StatusCode == http.StatusPartialContent {
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndex(cr, "/"); i > 0 {
				total, _ = strconv.ParseInt(cr[i+1:], 10, 64)
			}
		} else if total > 0 {
			total += have
		}
	}
	if total > 0 {
		if free := diskFree(s.lib.Temp); free >= 0 && free < total-have+(256<<20) {
			return permanent("not enough space on the server (%s free)", humanBytes(free))
		}
	}
	p := store.DownloadPatch{SizeBytes: i64p(max(total, 0)), Received: i64p(have)}
	if d.FileName == "" || resp.StatusCode == http.StatusOK {
		p.FileName = &name
	}
	_ = s.store.UpdateDownload(ctx, id, p)
	flags := os.O_CREATE | os.O_WRONLY
	if have == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	got := have
	lastSave := time.Now()
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return permanent("writing the file failed: %v", werr)
			}
			got += int64(n)
			if time.Since(lastSave) > 2*time.Second {
				lastSave = time.Now()
				_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Received: &got})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = s.store.UpdateDownload(context.WithoutCancel(ctx), id, store.DownloadPatch{Received: &got})
			return rerr
		}
	}
	if total > 0 && got < total {
		_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Received: &got})
		return fmt.Errorf("the connection ended early (%s of %s)", humanBytes(got), humanBytes(total))
	}
	return s.store.UpdateDownload(ctx, id, store.DownloadPatch{Received: &got, SizeBytes: &got})
}

// fileDownload files a finished download, or waits for the person to pick the system.
func (s *Server) fileDownload(ctx context.Context, id string) {
	d, err := s.store.GetDownload(ctx, 0, id)
	if err != nil {
		return
	}
	name := library.SafeName(d.FileName)
	if name == "" {
		name = "download"
	}
	if d.Platform == "" {
		choices := guessSystems(name, s.partPath(id))
		if len(choices) == 1 {
			d.Platform = choices[0]
			_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Platform: &d.Platform, Choices: choices})
		} else {
			if choices == nil {
				choices = []string{}
			}
			_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Status: strp("waiting"), Choices: choices})
			return
		}
	}
	up := &store.Upload{ID: id, UserID: d.UserID, Kind: "game", Platform: d.Platform, Rel: name, SizeBytes: d.Received}
	gameID, err := s.fileAndScan(context.WithoutCancel(ctx), d.UserID, up, true)
	if err != nil {
		s.failDownload(ctx, id, err)
		s.tellUpload(ctx, d.UserID, up, err)
		return
	}
	_ = s.store.UpdateDownload(ctx, id, store.DownloadPatch{Status: strp("done"), GameID: i64p(gameID)})
	if d.Received > 256<<20 {
		s.tellUpload(ctx, d.UserID, up, nil)
	}
}

// guessSystems says which systems a file can be for (looking into a zip too).
func guessSystems(name, path string) []string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext != "zip" {
		return platforms.ForExtension(ext)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer zr.Close()
	var out []string
	seen := map[string]bool{}
	for _, f := range zr.File {
		e := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Name), "."))
		if e == "" || platforms.Companion(e) && e != "cue" {
			continue
		}
		for _, p := range platforms.ForExtension(e) {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func hasGameExt(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return ext == "zip" || ext == "7z" || len(platforms.ForExtension(ext)) > 0
}

// fileNameFrom reads the file's name from the answer (Content-Disposition, else the address).
func fileNameFrom(resp *http.Response) string {
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		if n := params["filename"]; n != "" {
			return library.SafeName(filepath.Base(n))
		}
	}
	p := resp.Request.URL.Path
	if un, err := url.PathUnescape(path.Base(p)); err == nil && un != "/" && un != "." {
		return library.SafeName(un)
	}
	return "download"
}

// ---------- links → files ----------

var (
	reDriveFile = regexp.MustCompile(`/file/d/([A-Za-z0-9_-]{10,})`)
	reArchive   = regexp.MustCompile(`^/(details|download)/([^/]+)/?$`)
)

// resolveLink turns a share page into the file's own address.
func (s *Server) resolveLink(ctx context.Context, u *store.User, raw string) (string, error) {
	pu, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return "", errors.New("only http:// and https:// links can be fetched")
	}
	host := strings.ToLower(pu.Hostname())
	q := pu.Query()
	switch {
	case host == "drive.google.com" || host == "docs.google.com":
		id := q.Get("id")
		if m := reDriveFile.FindStringSubmatch(pu.Path); m != nil {
			id = m[1]
		}
		if id != "" {
			return "https://drive.usercontent.google.com/download?id=" + url.QueryEscape(id) + "&export=download&confirm=t", nil
		}
	case strings.HasSuffix(host, "dropbox.com"):
		q.Set("dl", "1")
		pu.RawQuery = q.Encode()
		return pu.String(), nil
	case strings.HasSuffix(host, "sharepoint.com") || host == "onedrive.live.com" || host == "1drv.ms":
		q.Set("download", "1")
		pu.RawQuery = q.Encode()
		return pu.String(), nil
	case host == "archive.org" || host == "www.archive.org":
		if m := reArchive.FindStringSubmatch(pu.Path); m != nil {
			return s.archiveFile(ctx, u, m[2])
		}
	}
	return pu.String(), nil
}

// archiveFile picks the game file of an archive.org item (its biggest original game file).
func (s *Server) archiveFile(ctx context.Context, u *store.User, item string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://archive.org/metadata/"+url.PathEscape(item), nil)
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	resp, err := s.linkClient(u).Do(req)
	if err != nil {
		return "", fmt.Errorf("archive.org could not be reached: %w", err)
	}
	defer resp.Body.Close()
	var m struct {
		Files []struct {
			Name   string `json:"name"`
			Size   string `json:"size"`
			Source string `json:"source"`
		} `json:"files"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&m); err != nil || len(m.Files) == 0 {
		return "", errors.New("archive.org knows no item by that name")
	}
	best, bestSize := "", int64(-1)
	for _, f := range m.Files {
		if f.Source != "original" || !hasGameExt(f.Name) {
			continue
		}
		n, _ := strconv.ParseInt(f.Size, 10, 64)
		if n > bestSize {
			best, bestSize = f.Name, n
		}
	}
	if best == "" {
		return "", errors.New("this archive.org item has no game file — copy the link of the file you want from its download list")
	}
	parts := strings.Split(best, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "https://archive.org/download/" + url.PathEscape(item) + "/" + strings.Join(parts, "/"), nil
}

// linkClient fetches links; addresses on the local network only for admins.
func (s *Server) linkClient(u *store.User) *http.Client {
	allowLocal := u != nil && u.Role == "admin"
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	if !allowLocal {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, _ := net.SplitHostPort(address)
			if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || isCGNAT(ip) {
				return permanent("addresses on the local network can only be fetched by an admin")
			}
			return nil
		}
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext, TLSHandshakeTimeout: 20 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second, MaxIdleConns: 4, IdleConnTimeout: 60 * time.Second}
	return &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return permanent("the link redirects too often")
		}
		return nil
	}}
}

func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64
}

// ---------- API ----------

func (s *Server) handleCreateDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		URL      string `json:"url"`
		Platform string `json:"platform"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.URL = strings.TrimSpace(in.URL)
	pu, err := url.Parse(in.URL)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		writeErr(w, http.StatusBadRequest, "paste a link that starts with http:// or https://")
		return
	}
	if in.Platform != "" && !reSlug.MatchString(in.Platform) {
		writeErr(w, http.StatusBadRequest, "unknown system")
		return
	}
	d := &store.Download{ID: store.RandomToken(16), UserID: u.ID, URL: in.URL, Platform: in.Platform}
	if err := s.store.CreateDownload(ctx, d); err != nil {
		s.fail(w, err, "download")
		return
	}
	s.wakeDownloads()
	d, _ = s.store.GetDownload(ctx, u.ID, d.ID)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListDownloads(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "downloads")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSetDownloadSystem picks the system of a download that waits for one (or try a failed one again).
func (s *Server) handleUpdateDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	d, err := s.store.GetDownload(ctx, u.ID, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err, "download")
		return
	}
	var in struct {
		Platform string `json:"platform"`
		Retry    bool   `json:"retry"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch {
	case in.Retry && d.Status == "failed":
		_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("queued"), Error: strp("")})
		s.wakeDownloads()
	case in.Platform != "":
		if !reSlug.MatchString(in.Platform) {
			writeErr(w, http.StatusBadRequest, "unknown system")
			return
		}
		_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Platform: &in.Platform})
		if d.Status == "waiting" {
			_ = s.store.UpdateDownload(ctx, d.ID, store.DownloadPatch{Status: strp("downloading")})
			go s.fileDownload(context.WithoutCancel(ctx), d.ID)
		}
	}
	d, _ = s.store.GetDownload(ctx, u.ID, d.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	d, err := s.store.GetDownload(ctx, u.ID, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err, "download")
		return
	}
	s.dlMu.Lock()
	if cancel := s.dlRunning[d.ID]; cancel != nil {
		cancel()
	}
	s.dlMu.Unlock()
	if d.Status != "done" {
		_ = os.Remove(s.partPath(d.ID))
	}
	_ = s.store.DeleteDownload(ctx, u.ID, d.ID)
	w.WriteHeader(http.StatusNoContent)
}
