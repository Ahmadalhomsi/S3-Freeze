package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"s3sync/internal/storage"
	"s3sync/internal/store"
)

type storageView struct {
	*store.Storage
	HasSecret bool `json:"has_secret"`
}

type storageInput struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	UseSSL    bool   `json:"use_ssl"`
	PathStyle bool   `json:"path_style"`
	LocalPath string `json:"local_path"`
}

// apply copies the input onto st. An empty secret keeps the existing one.
func (in *storageInput) apply(st *store.Storage) error {
	st.Name = strings.TrimSpace(in.Name)
	st.Type = in.Type
	st.Endpoint = strings.TrimSpace(in.Endpoint)
	st.Region = strings.TrimSpace(in.Region)
	st.AccessKey = strings.TrimSpace(in.AccessKey)
	if in.SecretKey != "" {
		st.SecretKey = in.SecretKey
	}
	st.UseSSL = in.UseSSL
	st.PathStyle = in.PathStyle
	st.LocalPath = strings.TrimSpace(in.LocalPath)
	if st.Name == "" {
		return errors.New("name is required")
	}
	switch st.Type {
	case "s3":
		if st.Endpoint == "" {
			return errors.New("endpoint is required")
		}
	case "local":
		if st.LocalPath == "" {
			return errors.New("path is required")
		}
	default:
		return errors.New(`type must be "s3" or "local"`)
	}
	return nil
}

func (s *Server) listStorages(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListStorages()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]storageView, len(list))
	for i, st := range list {
		out[i] = storageView{st, st.SecretKey != ""}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createStorage(w http.ResponseWriter, r *http.Request) {
	var in storageInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	st := &store.Storage{}
	if err := in.apply(st); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateStorage(st); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, storageView{st, st.SecretKey != ""})
}

func (s *Server) updateStorage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	st, err := s.store.GetStorage(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in storageInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.apply(st); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateStorage(st); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, storageView{st, st.SecretKey != ""})
}

func (s *Server) deleteStorage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	inUse, err := s.store.StorageInUse(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if inUse {
		writeError(w, http.StatusConflict, "storage is used by one or more backup jobs")
		return
	}
	if err := s.store.DeleteStorage(id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// testStorage checks connectivity for an unsaved (or edited) storage config.
func (s *Server) testStorage(w http.ResponseWriter, r *http.Request) {
	var in storageInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	st := &store.Storage{}
	if in.ID != 0 {
		existing, err := s.store.GetStorage(in.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		st = existing
	}
	if in.Name == "" {
		in.Name = "test"
	}
	if err := in.apply(st); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	be, err := storage.New(st.Config())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := timeoutCtx(r, 15*time.Second)
	defer cancel()
	if err := be.Test(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	buckets, _ := be.ListBuckets(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "buckets": len(buckets)})
}

func (s *Server) listBuckets(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	st, err := s.store.GetStorage(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	be, err := storage.New(st.Config())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := timeoutCtx(r, 15*time.Second)
	defer cancel()
	buckets, err := be.ListBuckets(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.Strings(buckets)
	if buckets == nil {
		buckets = []string{}
	}
	writeJSON(w, http.StatusOK, buckets)
}
