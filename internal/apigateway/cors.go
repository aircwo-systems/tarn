package apigateway

import (
	"errors"
	"slices"

	"github.com/aircwo-systems/tarn/internal/cors"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// DeleteCORSConfiguration removes an API's corsConfiguration.
func (s *Service) DeleteCORSConfiguration(apiID string) error {
	api, err := s.store.GetAPI(apiID)
	if err != nil {
		return err
	}
	api.CorsConfiguration = nil
	return s.store.SaveAPI(api)
}

// CORSPolicy returns the API's CORS policy, or nil when the API does not exist
// or has no corsConfiguration. Without one, preflights are routed like any
// other request and integration responses pass through unchanged.
func (s *Service) CORSPolicy(apiID string) *cors.Policy {
	api, err := s.store.GetAPI(apiID)
	if err != nil || api.CorsConfiguration == nil {
		return nil
	}
	c := api.CorsConfiguration
	return &cors.Policy{
		AllowOrigins:     c.AllowOrigins,
		AllowMethods:     c.AllowMethods,
		AllowHeaders:     c.AllowHeaders,
		ExposeHeaders:    c.ExposeHeaders,
		MaxAge:           c.MaxAge,
		AllowCredentials: c.AllowCredentials,
	}
}

func validateCORS(c *types.APIGatewayCORS) error {
	if c == nil {
		return nil
	}
	if c.AllowCredentials && slices.Contains(c.AllowOrigins, "*") {
		return errors.New("allowCredentials cannot be true when allowOrigins contains *")
	}
	if c.MaxAge < -1 || c.MaxAge > 86400 {
		return errors.New("maxAge must be between -1 and 86400")
	}
	return nil
}
