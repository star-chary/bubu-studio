package main

import (
	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/persistence"
)

// Images keep their existing Ark client; videos are selected from the immutable
// task input rather than the canvas's current model or the provider task ID.
type generators struct {
	*ark.Client
	apimartVideo persistence.VideoGenerator
	apimartImage *apimart.Client
}

func (g *generators) APIMartImageGenerator() persistence.APIMartImageGenerator { return g.apimartImage }

func (g *generators) VideoGeneratorFor(provider string) persistence.VideoGenerator {
	switch provider {
	case "", "ark":
		return g.Client
	case persistence.ProviderAPIMart:
		return g.apimartVideo
	}
	return nil
}
