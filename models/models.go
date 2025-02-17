package models

import (
	"bytes"
	"time"

	"github.com/traefik/yaegi/interp"
)

type Runtime struct {
	ID           string              `json:"id,omitempty"`
	Prompt       string              `json:"prompt,omitempty"`
	Code         string              `json:"code,omitempty"`
	State        string              `json:"state,omitempty"`
	LastErrorMsg string              `json:"lastErrorMsg,omitempty"`
	RebuildCount int                 `json:"rebuildCount,omitempty"`
	Executer     *interp.Interpreter `json:"-"`
	StopFunction func()              `json:"-"`
	Port         int                 `json:"port"`
	CreatedAt    time.Time           `json:"createdAt,omitempty"`
	StartedAt    time.Time           `json:"startedAt,omitempty"`
	FinishedAt   time.Time           `json:"finishedAt,omitempty"`
	Logs         *bytes.Buffer       `json:"logs,omitempty"`
}
