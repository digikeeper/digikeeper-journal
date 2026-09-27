package schemaregistry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	registry "github.com/digikeeper/digikeeper-journal/internal/schemaregistry"
)

type SchemaEntry struct {
	Type         string          `json:"type"`
	Version      int             `json:"version"`
	JSONSchema   json.RawMessage `json:"schema"`
	Instructions string          `json:"instructions"`
}
type SchemaSummary struct {
	Type          string `json:"type"`
	LatestVersion int    `json:"latest_version"`
	Versions      []int  `json:"versions"`
}
type Handler struct{ registry *registry.Registry }

func NewHandler(r *registry.Registry) *Handler { return &Handler{registry: r} }

type ListOutput struct {
	Body struct {
		Schemas []SchemaSummary `json:"schemas"`
	}
}

func (h *Handler) ListSchemas(_ context.Context, _ *struct{}) (*ListOutput, error) {
	out := &ListOutput{}
	for _, typeName := range h.registry.Types() {
		versions := h.registry.Versions(typeName)
		out.Body.Schemas = append(out.Body.Schemas, SchemaSummary{Type: typeName, LatestVersion: versions[len(versions)-1], Versions: versions})
	}
	return out, nil
}

type GetInput struct {
	Type string `path:"type" doc:"Schema type name"`
}
type GetVersionInput struct {
	Type    string `path:"type" doc:"Schema type name"`
	Version int    `path:"version" minimum:"1" doc:"Immutable schema version"`
}
type GetOutput struct{ Body SchemaEntry }

func (h *Handler) GetSchema(_ context.Context, input *GetInput) (*GetOutput, error) {
	version, err := h.registry.LatestVersion(input.Type)
	if err != nil {
		return nil, huma.Error404NotFound("schema type not found: " + input.Type)
	}
	return h.getSchema(input.Type, version)
}
func (h *Handler) GetSchemaVersion(_ context.Context, input *GetVersionInput) (*GetOutput, error) {
	return h.getSchema(input.Type, input.Version)
}
func (h *Handler) getSchema(typeName string, version int) (*GetOutput, error) {
	doc, err := h.registry.Entry(typeName, version)
	if err != nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("schema not found: %s version %d", typeName, version))
	}
	out := &GetOutput{}
	out.Body = SchemaEntry{Type: doc.Type, Version: doc.Version, JSONSchema: doc.Schema, Instructions: doc.Instructions}
	return out, nil
}
