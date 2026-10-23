package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func workflowParams(r *http.Request) (namespace, name, version string) {
	return chi.URLParam(r, "namespace"), chi.URLParam(r, "name"), chi.URLParam(r, "version")
}

func instanceID(r *http.Request) string {
	return chi.URLParam(r, "id")
}
