package k8s

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/convox/convox/pkg/manifest"
	"github.com/convox/convox/pkg/structs"
	cv "github.com/convox/convox/provider/k8s/pkg/client/clientset/versioned"
	ae "k8s.io/apimachinery/pkg/api/errors"
	am "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ScannerStartSize = 4096
	ScannerMaxSize   = 1024 * 1024
)

func (p *Provider) convoxClient() (cv.Interface, error) {
	return cv.NewForConfig(p.Config)
}

func (p *Provider) ingressSecrets(a *structs.App, ss manifest.Services) (map[string]string, error) {
	domains := map[string]bool{}

	for _, s := range ss {
		for _, d := range s.Domains {
			domains[d] = false
		}
	}

	cs, err := p.CertificateList()
	if err != nil {
		return nil, err
	}

	sort.Slice(cs, func(i, j int) bool { return cs[i].Expiration.After(cs[i].Expiration) })

	secrets := map[string]string{}

	for _, c := range cs {
		count := 0

		for _, d := range c.Domains {
			if v, ok := domains[d]; ok && !v {
				domains[d] = true
				count++
			}
		}

		if count > 0 {
			for _, d := range c.Domains {
				secrets[d] = c.Id
			}
		}
	}

	for d, matched := range domains {
		if !matched {
			c, err := p.CertificateGenerate([]string{d})
			if err != nil {
				return nil, err
			}

			secrets[d] = c.Id
		}
	}

	ids := map[string]bool{}

	for _, id := range secrets {
		ids[id] = true
	}

	for id := range ids {
		if _, err := p.Cluster.CoreV1().Secrets(p.AppNamespace(a.Name)).Get(id, am.GetOptions{}); ae.IsNotFound(err) {

			kc, err := p.Cluster.CoreV1().Secrets(p.Namespace).Get(id, am.GetOptions{})
			if err != nil {
				return nil, err
			}

			kc.ObjectMeta.Namespace = p.AppNamespace(a.Name)
			kc.ResourceVersion = ""
			kc.UID = ""

			if _, err := p.Cluster.CoreV1().Secrets(p.AppNamespace(a.Name)).Create(kc); err != nil {
				return nil, err
			}
		}
	}

	return secrets, nil
}

func (p *Provider) systemEnvironment(app, release string) (map[string]string, error) {
	senv := map[string]string{
		"APP":      app,
		"RACK":     p.Name,
		"RACK_URL": fmt.Sprintf("https://convox:%s@api.%s.svc.cluster.local:5443", p.Password, p.Namespace),
		"RELEASE":  release,
	}

	r, err := p.ReleaseGet(app, release)
	if err != nil {
		return nil, err
	}

	if r.Build != "" {
		b, err := p.BuildGet(app, r.Build)
		if err != nil {
			return nil, err
		}

		senv["BUILD"] = b.Id
		senv["BUILD_DESCRIPTION"] = b.Description
	}

	return senv, nil
}

func dockerSystemId() (string, error) {
	data, err := exec.Command("docker", "system", "info").CombinedOutput()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID: ") {
			return strings.ToLower(strings.TrimPrefix(line, "ID: ")), nil
		}
	}

	return "", fmt.Errorf("could not find docker system id")
}

func envName(s string) string {
	return strings.Replace(strings.ToUpper(s), "-", "_", -1)
}

type imageManifest []struct {
	RepoTags []string
}

func extractImageManifest(r io.Reader) (imageManifest, error) {
	mtr := tar.NewReader(r)

	var manifest imageManifest

	for {
		mh, err := mtr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if mh.Name == "manifest.json" {
			var mdata bytes.Buffer

			if _, err := io.Copy(&mdata, mtr); err != nil {
				return nil, err
			}

			if err := json.Unmarshal(mdata.Bytes(), &manifest); err != nil {
				return nil, err
			}

			return manifest, nil
		}
	}

	return nil, fmt.Errorf("unable to locate manifest")
}

func systemVolume(v string) bool {
	switch v {
	case "/var/run/docker.sock":
		return true
	case "/var/snap/microk8s/current/docker.sock":
		return true
	}
	return false
}

func (p *Provider) volumeFrom(app, service, v string) string {
	if from := strings.Split(v, ":")[0]; systemVolume(from) {
		return from
	} else if strings.Contains(v, ":") {
		return path.Join("/mnt/volumes", app, "app", from)
	} else {
		return path.Join("/mnt/volumes", app, "service", service, from)
	}
}

func (p *Provider) volumeName(app, v string) string {
	hash := sha256.Sum256([]byte(v))
	name := fmt.Sprintf("%s-%s-%x", p.Name, app, hash[0:20])
	if len(name) > 63 {
		name = name[0:62]
	}
	return name
}

func (p *Provider) volumeSources(app, service string, vs []string) []string {
	vsh := map[string]bool{}

	for _, v := range vs {
		vsh[p.volumeFrom(app, service, v)] = true
	}

	vsu := []string{}

	for v := range vsh {
		vsu = append(vsu, v)
	}

	sort.Strings(vsu)

	return vsu
}

func volumeTo(v string) (string, error) {
	switch parts := strings.SplitN(v, ":", 2); len(parts) {
	case 1:
		return parts[0], nil
	case 2:
		return parts[1], nil
	default:
		return "", fmt.Errorf("invalid volume %q", v)
	}
}
