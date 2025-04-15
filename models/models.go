package models

import (
	"bytes"
	"time"

	"github.com/traefik/yaegi/interp"
)

type Runtime struct {
	ID                string              `json:"id,omitempty"`
	Title             string              `json:"title,omitempty"`
	Prompt            string              `json:"prompt,omitempty"`
	Code              string              `json:"code,omitempty"`
	State             RuntimeState        `json:"state,omitempty"`
	LastErrorMsg      string              `json:"lastErrorMsg,omitempty"`
	RebuildCount      int                 `json:"rebuildCount,omitempty"`
	Executer          *interp.Interpreter `json:"-"`
	StopFunction      func()              `json:"-"`
	Port              int                 `json:"port"`
	CreatedAt         time.Time           `json:"createdAt,omitempty,omitzero"`
	StartedAt         time.Time           `json:"startedAt,omitempty,omitzero"`
	FinishedAt        time.Time           `json:"finishedAt,omitempty,omitzero"`
	Logs              *bytes.Buffer       `json:"logs,omitempty"`
	PassedHealthCheck bool                `json:"passedHealthCheck"`
}

type RuntimeState string

const (
	RSINIT RuntimeState = "initializing"
	RSRDY  RuntimeState = "ready"
	RSRUN  RuntimeState = "running"
	RSSTOP RuntimeState = "stopped"
	RSERR  RuntimeState = "error"
	RSDONE RuntimeState = "done"
)
