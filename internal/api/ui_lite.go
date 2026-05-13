//go:build lite

package api

import "net/http"

func (s *Server) registerUIRoutes(_ *http.ServeMux) {}
