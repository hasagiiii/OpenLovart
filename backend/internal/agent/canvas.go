package agent

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/jiantaoli/openlovart/backend/internal/business/aiimage"
	"github.com/jiantaoli/openlovart/backend/internal/business/canvas_elements"
)

// CanvasPersister persists a tool-generated image as a canvas element owned by
// a project, returning the created element id. Implementations enforce that
// projectID is owned by userID and return an error otherwise (the tool then
// just omits the element id and still returns the URL).
type CanvasPersister interface {
	PersistImage(ctx context.Context, userID, projectID uuid.UUID, img aiimage.Image, index int) (string, error)
}

// repoCanvasPersister adapts canvas_elements.Repo to CanvasPersister.
type repoCanvasPersister struct {
	repo *canvas_elements.Repo
}

// NewCanvasPersister wraps a canvas_elements.Repo as a CanvasPersister.
func NewCanvasPersister(repo *canvas_elements.Repo) CanvasPersister {
	return &repoCanvasPersister{repo: repo}
}

// PersistImage builds a frontend-shaped image element and appends it.
func (p *repoCanvasPersister) PersistImage(ctx context.Context, userID, projectID uuid.UUID, img aiimage.Image, index int) (string, error) {
	width := img.Width
	if width <= 0 {
		width = 512
	}
	height := img.Height
	if height <= 0 {
		height = 512
	}

	// element_data mirrors the frontend CanvasElement shape (type "image",
	// content = URL); the id field is filled in below from the inserted row.
	data := map[string]any{
		"type":    "image",
		"x":       index * 40,
		"y":       index * 40,
		"content": img.URL,
		"width":   width,
		"height":  height,
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	rows, err := p.repo.AppendForProject(ctx, projectID, userID, []canvas_elements.ElementInput{
		{ElementData: raw},
	})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].ID.String(), nil
}
