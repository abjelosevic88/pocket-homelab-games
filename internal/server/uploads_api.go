package server

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-games/internal/library"
	"github.com/abjelosevic88/pocket-homelab-games/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-games/internal/platforms"
	"github.com/abjelosevic88/pocket-homelab-games/internal/store"
)

// Uploads are resumable: the browser creates one per file, sends it in
// chunks at an offset (a dropped connection continues where it stopped) and
// finishes it, which files it into the library and scans the platform.
// Partial files live in LIBRARY_DIR/.uploads, on the same disk, so filing is
// a rename even for a 40 GB disc set.

const maxChunk = 64 << 20

func (s *Server) partPath(id string) string { return filepath.Join(s.lib.Temp, id+".part") }

// handleGuessPlatform says which systems a file name can belong to.
func (s *Server) handleGuessPlatform(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	cands := platforms.ForExtension(ext)
	if cands == nil {
		cands = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"extension": ext, "platforms": cands})
}

func (s *Server) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Kind     string `json:"kind"`     // game | firmware
		Platform string `json:"platform"` // folder name
		Name     string `json:"name"`     // "Game.gba" or "Game (Europe)/Disc 1.chd"
		Size     int64  `json:"size"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Kind == "" {
		in.Kind = "game"
	}
	if in.Kind != "game" && in.Kind != "firmware" {
		writeErr(w, http.StatusBadRequest, "kind is game or firmware")
		return
	}
	if in.Kind == "firmware" && u.Role != "admin" {
		writeErr(w, http.StatusForbidden, "only an admin can add BIOS files")
		return
	}
	if !reSlug.MatchString(in.Platform) {
		writeErr(w, http.StatusBadRequest, "choose the system the game is for")
		return
	}
	rel := library.SafeRel(in.Name)
	if rel == "" || in.Size <= 0 {
		writeErr(w, http.StatusBadRequest, "a file name and size are needed")
		return
	}
	if in.Kind == "firmware" {
		rel = filepath.Base(rel)
	}
	target := s.uploadTarget(in.Kind, in.Platform, rel)
	if _, err := os.Stat(target); err == nil {
		writeErr(w, http.StatusConflict, fmt.Sprintf("%q is already in the library", rel))
		return
	}
	if err := os.MkdirAll(s.lib.Temp, 0o755); err != nil {
		s.fail(w, err, "upload folder")
		return
	}
	if free := diskFree(s.lib.Temp); free >= 0 && free < in.Size+(256<<20) {
		writeErr(w, http.StatusInsufficientStorage, fmt.Sprintf("not enough space on the server (%s free)", humanBytes(free)))
		return
	}
	up := &store.Upload{ID: store.RandomToken(16), UserID: u.ID, Kind: in.Kind, Platform: in.Platform, Rel: rel, SizeBytes: in.Size}
	f, err := os.Create(s.partPath(up.ID))
	if err != nil {
		s.fail(w, err, "upload")
		return
	}
	f.Close()
	if err := s.store.CreateUpload(ctx, up); err != nil {
		_ = os.Remove(s.partPath(up.ID))
		s.fail(w, err, "upload")
		return
	}
	up, _ = s.store.GetUpload(ctx, u.ID, up.ID)
	writeJSON(w, http.StatusCreated, up)
}

func (s *Server) uploadTarget(kind, platform, rel string) string {
	if kind == "firmware" {
		return s.firmwarePath(platform, rel)
	}
	return filepath.Join(s.platformDir(platform), filepath.FromSlash(rel))
}

func (s *Server) uploadFor(w http.ResponseWriter, r *http.Request) (*store.Upload, bool) {
	up, err := s.store.GetUpload(r.Context(), userFrom(r.Context()).ID, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err, "upload")
		return nil, false
	}
	return up, true
}

func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	if up, ok := s.uploadFor(w, r); ok {
		writeJSON(w, http.StatusOK, up)
	}
}

// handleUploadChunk writes the body at ?offset= (which must not leave a gap).
func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	up, ok := s.uploadFor(w, r)
	if !ok {
		return
	}
	if up.Status != "open" {
		writeErr(w, http.StatusConflict, "this upload is finished")
		return
	}
	off, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || off < 0 || off > up.Received || off >= up.SizeBytes {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "send from the offset the server has", "received": up.Received})
		return
	}
	f, err := os.OpenFile(s.partPath(up.ID), os.O_WRONLY, 0o644)
	if err != nil {
		s.fail(w, err, "upload")
		return
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		s.fail(w, err, "upload")
		return
	}
	limit := min(int64(maxChunk), up.SizeBytes-off)
	n, err := io.Copy(f, io.LimitReader(r.Body, limit))
	if n > 0 && off+n > up.Received {
		up.Received = off + n
		_ = s.store.UploadProgress(r.Context(), up.ID, up.Received)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the connection broke: " + err.Error(), "received": up.Received})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"received": up.Received, "size_bytes": up.SizeBytes})
}

// handleFinishUpload files the upload. ?scan=0 for all but the last file of
// a multi-file game, so a half-uploaded folder never shows up as a game.
func (s *Server) handleFinishUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	up, ok := s.uploadFor(w, r)
	if !ok {
		return
	}
	if up.Status != "open" {
		writeJSON(w, http.StatusOK, up)
		return
	}
	if up.Received != up.SizeBytes {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "not all of the file arrived yet", "received": up.Received})
		return
	}
	target, err := s.fileUpload(up)
	if err != nil {
		_ = s.store.FinishUpload(ctx, up.ID, "failed", err.Error(), 0)
		_ = os.Remove(s.partPath(up.ID))
		s.tellUpload(ctx, u.ID, up, err)
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	var gameID int64
	if up.Kind == "firmware" {
		s.scanFirmware(ctx)
	} else if r.URL.Query().Get("scan") != "0" {
		top := strings.SplitN(filepath.ToSlash(target), "/", 2)[0]
		_ = s.scanAll(context.WithoutCancel(ctx), map[string]int64{up.Platform + "/" + top: u.ID})
		if pl, err := s.store.GetPlatform(ctx, up.Platform); err == nil {
			if g, err := s.store.GameByPath(ctx, pl.ID, top); err == nil {
				gameID = g.ID
			} else if g, err := s.findLooseGame(ctx, pl.ID, top); err == nil {
				gameID = g.ID
			}
		}
	}
	_ = s.store.FinishUpload(ctx, up.ID, "done", "", gameID)
	if up.SizeBytes > 256<<20 {
		s.tellUpload(ctx, u.ID, up, nil)
	}
	up, _ = s.store.GetUpload(ctx, u.ID, up.ID)
	writeJSON(w, http.StatusOK, up)
}

// findLooseGame finds the game a loose file joined ("Game (Track 2).bin" → "Game.cue").
func (s *Server) findLooseGame(ctx context.Context, platformID int64, file string) (*store.Game, error) {
	var id int64
	err := s.store.DB.QueryRowContext(ctx, `SELECT game_id FROM game_files f JOIN games g ON g.id = f.game_id WHERE g.platform_id = ? AND g.is_folder = 0 AND f.rel = ?`, platformID, file).Scan(&id)
	if err != nil {
		return nil, store.ErrNotFound
	}
	return s.store.GetGame(ctx, 0, id)
}

// fileUpload moves the finished file into the library and returns its path
// relative to the platform folder (a zip of a disc game becomes a folder).
func (s *Server) fileUpload(up *store.Upload) (string, error) {
	part := s.partPath(up.ID)
	target := s.uploadTarget(up.Kind, up.Platform, up.Rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	isZip := strings.EqualFold(filepath.Ext(up.Rel), ".zip")
	p := platforms.Get(up.Platform)
	switch {
	case up.Kind == "firmware" && isZip:
		err := unzipFlat(part, filepath.Dir(target))
		_ = os.Remove(part)
		return filepath.Base(up.Rel), err
	case up.Kind == "game" && isZip && !strings.Contains(up.Rel, "/") && p.Disc && zipHasDisc(part):
		// a zipped disc can't be played by most emulators: unpack it into a folder
		dir := filepath.Join(s.platformDir(up.Platform), library.SafeName(library.StripExt(up.Rel)))
		if _, err := os.Stat(dir); err == nil {
			_ = os.Remove(part)
			return "", fmt.Errorf("%q is already in the library", filepath.Base(dir))
		}
		if err := unzipInto(part, dir); err != nil {
			_ = os.RemoveAll(dir)
			_ = os.Remove(part)
			return "", fmt.Errorf("the zip could not be opened: %w", err)
		}
		_ = os.Remove(part)
		return filepath.Base(dir), nil
	}
	if _, err := os.Stat(target); err == nil {
		_ = os.Remove(part)
		return "", fmt.Errorf("%q is already in the library", up.Rel)
	}
	if err := os.Rename(part, target); err != nil {
		return "", err
	}
	return up.Rel, nil
}

var discExt = map[string]bool{"cue": true, "bin": true, "iso": true, "chd": true, "gdi": true, "cdi": true, "m3u": true, "img": true, "ccd": true, "rvz": true, "cso": true, "mdf": true, "mds": true, "pbp": true, "wbfs": true}

func zipHasDisc(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if discExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Name), "."))] {
			return true
		}
	}
	return false
}

// unzipInto unpacks a zip into dir (keeping its folders, never escaping dir).
func unzipInto(path, dir string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "__MACOSX/") || strings.HasPrefix(filepath.Base(f.Name), ".") {
			continue
		}
		rel := library.SafeRel(f.Name)
		if rel == "" {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := extractOne(f, dst); err != nil {
			return err
		}
	}
	flattenSingleFolder(dir)
	return nil
}

// flattenSingleFolder: a zip holding one folder with everything in it unpacks
// into that folder's contents.
func flattenSingleFolder(dir string) {
	des, err := os.ReadDir(dir)
	if err != nil || len(des) != 1 || !des[0].IsDir() {
		return
	}
	inner := filepath.Join(dir, des[0].Name())
	items, _ := os.ReadDir(inner)
	for _, it := range items {
		_ = os.Rename(filepath.Join(inner, it.Name()), filepath.Join(dir, it.Name()))
	}
	_ = os.Remove(inner)
}

// unzipFlat unpacks the files of a zip into dir without folders, never
// overwriting a file that is already there (BIOS packs).
func unzipFlat(path, dir string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "__MACOSX/") || strings.HasPrefix(base, ".") {
			continue
		}
		dst := filepath.Join(dir, library.SafeName(base))
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := extractOne(f, dst); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return errors.New("nothing new in the zip")
	}
	return nil
}

func extractOne(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && !f.Modified.IsZero() {
		_ = os.Chtimes(dst, f.Modified, f.Modified)
	}
	return err
}

func (s *Server) handleCancelUpload(w http.ResponseWriter, r *http.Request) {
	up, ok := s.uploadFor(w, r)
	if !ok {
		return
	}
	_ = os.Remove(s.partPath(up.ID))
	_ = s.store.DeleteUpload(r.Context(), up.ID)
	w.WriteHeader(http.StatusNoContent)
}

// dropStaleUploads forgets uploads nobody touched for two days.
func (s *Server) dropStaleUploads(ctx context.Context) {
	ids, err := s.store.StaleUploads(ctx, time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return
	}
	for _, id := range ids {
		_ = os.Remove(s.partPath(id))
		_ = s.store.DeleteUpload(ctx, id)
	}
	_, _ = s.store.DB.ExecContext(ctx, `DELETE FROM uploads WHERE status != 'open' AND updated_at < ?`, time.Now().Add(-7*24*time.Hour).UTC().Format(time.RFC3339Nano))
}

func (s *Server) tellUpload(ctx context.Context, uid int64, up *store.Upload, err error) {
	st, _ := s.store.GetSettings(ctx, uid)
	if !st.NotifyUploads || st.NotifyURL == "" {
		return
	}
	m := notify.Message{Title: "Upload finished", Body: filepath.Base(up.Rel) + " is in your library.", Priority: "success", Tags: []string{"video_game"}, Link: s.cfg.BaseURL + "/library?sort=added"}
	if err != nil {
		m = notify.Message{Title: "Upload failed", Body: filepath.Base(up.Rel) + ": " + err.Error(), Priority: "failure", Tags: []string{"warning"}}
	}
	_ = notify.New(st.NotifyURL, st.NotifyFormat).Send(context.WithoutCancel(ctx), m)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
