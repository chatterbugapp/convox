package k8s

import (
	"fmt"
	"strconv"
	"time"

	"github.com/convox/convox/pkg/common"
	"github.com/convox/convox/pkg/manifest"
	"github.com/convox/convox/pkg/structs"
	am "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (p *Provider) ServiceHost(app string, s manifest.Service) string {
	if s.Internal {
		return fmt.Sprintf("%s.%s-%s.svc.cluster.local", s.Name, p.Name, app)
	} else {
		return fmt.Sprintf("%s.%s.%s", s.Name, app, p.Domain)
	}
}

func (p *Provider) ServiceList(app string) (structs.Services, error) {
	lopts := am.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s,type=service", app),
	}

	ds, err := p.Cluster.AppsV1().Deployments(p.AppNamespace(app)).List(lopts)
	if err != nil {
		return nil, err
	}

	a, err := p.AppGet(app)
	if err != nil {
		return nil, err
	}

	if a.Release == "" {
		return structs.Services{}, nil
	}

	m, _, err := common.ReleaseManifest(p, app, a.Release)
	if err != nil {
		return nil, err
	}

	ss := structs.Services{}

	for _, d := range ds.Items {
		cs := d.Spec.Template.Spec.Containers

		if len(cs) != 1 || cs[0].Name != "main" {
			return nil, fmt.Errorf("unexpected containers for service: %s", d.ObjectMeta.Name)
		}

		// fmt.Printf("d.Spec = %+v\n", d.Spec)
		// fmt.Printf("d.Status = %+v\n", d.Status)

		s := structs.Service{
			Count: int(common.DefaultInt32(d.Spec.Replicas, 0)),
			Name:  d.ObjectMeta.Name,
			Ports: []structs.ServicePort{},
		}

		if len(cs[0].Ports) == 1 {
			// i, err := p.Cluster.ExtensionsV1beta1().Ingresses(p.AppNamespace(app)).Get(app, am.GetOptions{})
			// if err != nil {
			//   return nil, err
			// }
			ms, err := m.Service(d.ObjectMeta.Name)
			if err != nil {
				return nil, err
			}

			s.Domain = p.Engine.ServiceHost(app, *ms)

			// s.Domain = fmt.Sprintf("%s.%s", p.Engine.ServiceHost(app, s.Name), common.CoalesceString(i.Annotations["convox.domain"], i.Labels["rack"]))

			// if domain, ok := i.Annotations["convox.domain"]; ok {
			//   s.Domain += fmt.Sprintf(".%s", domain)
			// }

			cp := int(cs[0].Ports[0].ContainerPort)

			if ms.Internal {
				s.Ports = append(s.Ports, structs.ServicePort{Balancer: cp, Container: cp})
			} else {
				s.Ports = append(s.Ports, structs.ServicePort{Balancer: 443, Container: cp})
			}
		}

		ss = append(ss, s)
	}

	return ss, nil
}

func (p *Provider) ServiceRestart(app, name string) error {
	m, _, err := common.AppManifest(p, app)
	if err != nil {
		return err
	}

	s, err := m.Service(name)
	if err != nil {
		return err
	}

	if s.Agent.Enabled {
		return p.serviceRestartDaemonset(app, name)
	}

	return p.serviceRestartDeployment(app, name)
}

func (p *Provider) serviceRestartDaemonset(app, name string) error {
	ds := p.Cluster.ExtensionsV1beta1().DaemonSets(p.AppNamespace(app))

	s, err := ds.Get(name, am.GetOptions{})
	if err != nil {
		return err
	}

	if s.Spec.Template.Annotations == nil {
		s.Spec.Template.Annotations = map[string]string{}
	}

	s.Spec.Template.Annotations["convox.com/restart"] = strconv.FormatInt(time.Now().UTC().UnixNano(), 10)

	if _, err := ds.Update(s); err != nil {
		return err
	}

	return nil
}

func (p *Provider) serviceRestartDeployment(app, name string) error {
	ds := p.Cluster.ExtensionsV1beta1().Deployments(p.AppNamespace(app))

	s, err := ds.Get(name, am.GetOptions{})
	if err != nil {
		return err
	}

	if s.Spec.Template.Annotations == nil {
		s.Spec.Template.Annotations = map[string]string{}
	}

	s.Spec.Template.Annotations["convox.com/restart"] = strconv.FormatInt(time.Now().UTC().UnixNano(), 10)

	if _, err := ds.Update(s); err != nil {
		return err
	}

	return nil
}

func (p *Provider) ServiceUpdate(app, name string, opts structs.ServiceUpdateOptions) error {
	d, err := p.Cluster.AppsV1().Deployments(p.AppNamespace(app)).Get(name, am.GetOptions{})
	if err != nil {
		return err
	}

	if opts.Count != nil {
		c := int32(*opts.Count)
		d.Spec.Replicas = &c
	}

	if _, err := p.Cluster.AppsV1().Deployments(p.AppNamespace(app)).Update(d); err != nil {
		return err
	}

	return nil
}

func (p *Provider) serviceInstall(app, release, service string) error {
	a, err := p.AppGet(app)
	if err != nil {
		return err
	}

	m, r, err := common.ReleaseManifest(p, app, release)
	if err != nil {
		return err
	}

	s, err := m.Service(service)
	if err != nil {
		return err
	}

	if s.Port.Port == 0 {
		return nil
	}

	params := map[string]interface{}{
		"Namespace": p.AppNamespace(a.Name),
		"Release":   r,
		"Service":   s,
	}

	data, err := p.RenderTemplate("app/port", params)
	if err != nil {
		return err
	}

	if err := p.Apply(p.AppNamespace(app), fmt.Sprintf("service.%s", service), r.Id, data, fmt.Sprintf("system=convox,provider=k8s,rack=%s,app=%s,release=%s", p.Name, app, r.Id), 30); err != nil {
		return err
	}

	return nil
}
