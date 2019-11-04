package azure

import (
	"context"
	"encoding/base64"
	"os"

	"github.com/convox/convox/pkg/structs"
	"github.com/convox/convox/provider/k8s"
	"k8s.io/apimachinery/pkg/util/runtime"
)

type Provider struct {
	*k8s.Provider

	Bucket   string
	Key      []byte
	Project  string
	Region   string
	Registry string

	// LogAdmin *logadmin.Client
	// Logging  *logging.Client
	// Storage  *storage.Client
}

func FromEnv() (*Provider, error) {
	k, err := k8s.FromEnv()
	if err != nil {
		return nil, err
	}

	p := &Provider{
		Provider: k,
		Bucket:   os.Getenv("BUCKET"),
		Project:  os.Getenv("PROJECT"),
		Region:   os.Getenv("REGION"),
		Registry: os.Getenv("REGISTRY"),
	}

	key, err := base64.StdEncoding.DecodeString(os.Getenv("KEY"))
	if err != nil {
		return nil, err
	}

	p.Key = key

	k.Engine = p

	return p, nil
}

func (p *Provider) Initialize(opts structs.ProviderOptions) error {
	if err := p.initializeAzureServices(); err != nil {
		return err
	}

	if err := p.Provider.Initialize(opts); err != nil {
		return err
	}

	runtime.ErrorHandlers = []func(error){}

	return nil
}

func (p *Provider) WithContext(ctx context.Context) structs.Provider {
	pp := *p
	pp.Provider = pp.Provider.WithContext(ctx).(*k8s.Provider)
	return &pp
}

func (p *Provider) initializeAzureServices() error {
	return nil
}
