package gcp

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/convox/convox/pkg/structs"
)

var sequenceTokens sync.Map

func (p *Provider) Log(app, stream string, ts time.Time, message string) error {
	return nil
}

func (p *Provider) AppLogs(name string, opts structs.LogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("unimplemented")
}

// func (p *Provider) BuildLogs(app, id string, opts structs.LogsOptions) (io.ReadCloser, error) {
// 	return nil, fmt.Errorf("unimplemented")
// }

func (p *Provider) SystemLogs(opts structs.LogsOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("unimplemented")
}
