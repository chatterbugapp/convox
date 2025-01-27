package k8s_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/convox/convox/pkg/atom"
	"github.com/convox/convox/pkg/manifest"
	"github.com/convox/convox/pkg/structs"
	"github.com/convox/convox/provider/k8s"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"
	ac "k8s.io/api/core/v1"
	am "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func reformatYaml(data []byte) ([]byte, error) {
	rms := [][]byte{}

	parts := bytes.Split(data, []byte("---\n"))

	for _, part := range parts {
		var v interface{}

		if err := yaml.Unmarshal(part, &v); err != nil {
			return nil, err
		}

		data, err := yaml.Marshal(v)
		if err != nil {
			return nil, err
		}

		rms = append(rms, data)
	}

	return bytes.Join(rms, []byte("---\n")), nil
}

func requireYamlFixture(t *testing.T, d1 []byte, filename string) {
	r1, err := reformatYaml(d1)
	require.NoError(t, err)

	d2, err := ioutil.ReadFile(filepath.Join("testdata", filename))
	require.NoError(t, err)

	r2, err := reformatYaml(d2)
	require.NoError(t, err)

	require.Equal(t, string(r2), string(r1))
}

func testProvider(t *testing.T, fn func(*k8s.Provider)) {
	a := &atom.MockInterface{}
	c := fake.NewSimpleClientset()

	p := &k8s.Provider{
		Atom:      a,
		Cluster:   c,
		Domain:    "domain1",
		Engine:    &TestEngine{},
		Name:      "rack1",
		Namespace: "ns1",
		Provider:  "test",
	}

	err := p.Initialize(structs.ProviderOptions{})
	require.NoError(t, err)

	_, err = c.CoreV1().Namespaces().Create(&ac.Namespace{ObjectMeta: am.ObjectMeta{Name: "test"}})
	require.NoError(t, err)

	os.Setenv("NAMESPACE", "test")

	fn(p)

	a.AssertExpectations(t)
}

func testProviderManual(t *testing.T, fn func(*k8s.Provider, *fake.Clientset)) {
	c := &fake.Clientset{}

	p := &k8s.Provider{
		Cluster: c,
	}

	fn(p, c)
}

type TestEngine struct {
}

func (tr *TestEngine) AppIdles(app string) (bool, error) {
	return false, nil
}

func (te *TestEngine) AppParameters() map[string]string {
	return map[string]string{"Test": "foo"}
}

func (te *TestEngine) AppStatus(app string) (string, error) {
	return "statusing", nil
}

func (te *TestEngine) Heartbeat() (map[string]interface{}, error) {
	return map[string]interface{}{"foo": "bar"}, nil
}

func (te *TestEngine) IngressAnnotations(app string) (map[string]string, error) {
	return map[string]string{"ann1": "val1"}, nil
}

func (te *TestEngine) IngressSecrets(app string) ([]string, error) {
	return []string{}, nil
}

func (te *TestEngine) Log(app, stream string, ts time.Time, message string) error {
	return nil
}

func (te *TestEngine) ManifestValidate(m *manifest.Manifest) error {
	return nil
}

func (te *TestEngine) RepositoryAuth(app string) (string, string, error) {
	return "un1", "pw1", nil
}

func (te *TestEngine) RepositoryHost(app string) (string, bool, error) {
	return "repo1", true, nil
}

func (te *TestEngine) Resolver() (string, error) {
	return "", fmt.Errorf("no resolver")
}

func (te *TestEngine) ServiceHost(app string, s manifest.Service) string {
	return "service.host"
}

func (te *TestEngine) SystemHost() string {
	return "system.host"
}

func (te *TestEngine) SystemStatus() (string, error) {
	return "amazing", nil
}
