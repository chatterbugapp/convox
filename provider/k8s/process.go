package k8s

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/convox/convox/pkg/common"
	"github.com/convox/convox/pkg/options"
	"github.com/convox/convox/pkg/structs"
	shellquote "github.com/kballard/go-shellquote"
	ac "k8s.io/api/core/v1"
	am "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
)

func (p *Provider) ProcessExec(app, pid, command string, rw io.ReadWriter, opts structs.ProcessExecOptions) (int, error) {
	req := p.Cluster.CoreV1().RESTClient().Post().Resource("pods").Name(pid).Namespace(p.AppNamespace(app)).SubResource("exec").Param("container", "main")

	cp, err := shellquote.Split(command)
	if err != nil {
		return 0, err
	}

	if common.DefaultBool(opts.Entrypoint, false) {
		ps, err := p.ProcessGet(app, pid)
		if err != nil {
			return 0, err
		}

		r, err := p.ReleaseGet(app, ps.Release)
		if err != nil {
			return 0, err
		}

		b, err := p.BuildGet(app, r.Build)
		if err != nil {
			return 0, err
		}

		if b.Entrypoint != "" {
			ep, err := shellquote.Split(b.Entrypoint)
			if err != nil {
				return 0, err
			}

			cp = append(ep, cp...)
		}
	}

	eo := &ac.PodExecOptions{
		Container: "main",
		Command:   cp,
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}

	req.VersionedParams(eo, scheme.ParameterCodec)

	e, err := remotecommand.NewSPDYExecutor(p.Config, "POST", req.URL())
	if err != nil {
		return 0, err
	}

	inr, inw := io.Pipe()
	go io.Copy(inw, rw)

	sopts := remotecommand.StreamOptions{
		Stdin:  inr,
		Stdout: rw,
		Stderr: rw,
		Tty:    true,
	}

	if opts.Height != nil && opts.Width != nil {
		sopts.TerminalSizeQueue = &terminalSize{Height: *opts.Height, Width: *opts.Width}
	}

	err = e.Stream(sopts)
	if ee, ok := err.(exec.ExitError); ok {
		return ee.ExitStatus(), nil
	}
	if err != nil {
		return 0, err
	}

	return 0, nil
}

func (p *Provider) ProcessGet(app, pid string) (*structs.Process, error) {
	pd, err := p.Cluster.CoreV1().Pods(p.AppNamespace(app)).Get(pid, am.GetOptions{})
	if err != nil {
		return nil, err
	}

	ps, err := processFromPod(*pd)
	if err != nil {
		return nil, err
	}

	return ps, nil
}

func (p *Provider) ProcessList(app string, opts structs.ProcessListOptions) (structs.Processes, error) {
	filters := []string{
		"type!=resource",
	}

	if opts.Release != nil {
		filters = append(filters, fmt.Sprintf("release=%s", *opts.Release))
	}

	if opts.Service != nil {
		filters = append(filters, fmt.Sprintf("service=%s", *opts.Service))
	}

	pds, err := p.Cluster.CoreV1().Pods(p.AppNamespace(app)).List(am.ListOptions{LabelSelector: strings.Join(filters, ",")})
	if err != nil {
		return nil, err
	}

	pss := structs.Processes{}

	for _, pd := range pds.Items {
		ps, err := processFromPod(pd)
		if err != nil {
			return nil, err
		}

		pss = append(pss, *ps)
	}

	return pss, nil
}

func (p *Provider) ProcessLogs(app, pid string, opts structs.LogsOptions) (io.ReadCloser, error) {
	r, w := io.Pipe()

	go p.streamProcessLogs(w, app, pid, opts)

	return r, nil
}

func (p *Provider) streamProcessLogs(w io.WriteCloser, app, pid string, opts structs.LogsOptions) {
	defer w.Close()

	lopts := &ac.PodLogOptions{
		Follow:     true,
		Timestamps: true,
	}

	if opts.Since != nil {
		since := am.NewTime(time.Now().UTC().Add(*opts.Since))
		lopts.SinceTime = &since
	}

	service := ""

	for {
		pp, err := p.Cluster.CoreV1().Pods(p.AppNamespace(app)).Get(pid, am.GetOptions{})
		if err != nil {
			fmt.Printf("err: %+v\n", err)
			break
		}

		service = pp.Labels["service"]

		if pp.Status.Phase != "Pending" {
			break
		}

		time.Sleep(1 * time.Second)
	}

	for {
		r, err := p.Cluster.CoreV1().Pods(p.AppNamespace(app)).GetLogs(pid, lopts).Stream()
		if err != nil {
			fmt.Printf("err: %+v\n", err)
			break
		}

		s := bufio.NewScanner(r)

		s.Buffer(make([]byte, ScannerStartSize), ScannerMaxSize)

		for s.Scan() {
			line := s.Text()

			parts := strings.SplitN(line, " ", 2)
			if len(parts) != 2 {
				fmt.Printf("err: short line\n")
				continue
			}

			ts, err := time.Parse(time.RFC3339Nano, parts[0])
			if err != nil {
				fmt.Printf("err: %+v\n", err)
				continue
			}

			prefix := ""

			since := am.NewTime(ts)
			lopts.SinceTime = &since

			if common.DefaultBool(opts.Prefix, false) {
				prefix = fmt.Sprintf("%s %s ", ts.Format(time.RFC3339), fmt.Sprintf("service/%s/%s", service, pid))
			}

			fmt.Fprintf(w, "%s%s\n", prefix, strings.TrimSuffix(parts[1], "\n"))
		}

		if err := s.Err(); err != nil {
			fmt.Printf("err: %+v\n", err)
			continue
		}

		return
	}
}

func (p *Provider) ProcessRun(app, service string, opts structs.ProcessRunOptions) (*structs.Process, error) {
	s, err := p.podSpecFromRunOptions(app, service, opts)
	if err != nil {
		return nil, err
	}

	// ns, err := p.Cluster.CoreV1().Namespaces().Get(p.Namespace, am.GetOptions{})
	// if err != nil {
	// 	return nil, err
	// }

	release := common.DefaultString(opts.Release, "")

	if release == "" {
		a, err := p.AppGet(app)
		if err != nil {
			return nil, err
		}

		release = a.Release
	}

	pd, err := p.Cluster.CoreV1().Pods(p.AppNamespace(app)).Create(&ac.Pod{
		ObjectMeta: am.ObjectMeta{
			Annotations: map[string]string{
				// "iam.amazonaws.com/role": ns.ObjectMeta.Annotations["convox.aws.role"],
			},
			GenerateName: fmt.Sprintf("%s-", service),
			Labels: map[string]string{
				"app":     app,
				"rack":    p.Name,
				"release": release,
				"service": service,
				"system":  "convox",
				"type":    "process",
				"name":    service,
			},
		},
		Spec: *s,
	})
	if err != nil {
		return nil, err
	}

	ps, err := p.ProcessGet(app, pd.ObjectMeta.Name)
	if err != nil {
		return nil, err
	}

	return ps, nil
}

func (p *Provider) ProcessStop(app, pid string) error {
	if err := p.Cluster.CoreV1().Pods(p.AppNamespace(app)).Delete(pid, nil); err != nil {
		return err
	}

	return nil
}

func (p *Provider) ProcessWait(app, pid string) (int, error) {
	for {
		pd, err := p.Cluster.CoreV1().Pods(p.AppNamespace(app)).Get(pid, am.GetOptions{})
		if err != nil {
			return 0, err
		}

		cs := pd.Status.ContainerStatuses

		if len(cs) != 1 || cs[0].Name != "main" {
			return 0, fmt.Errorf("unexpected containers for pid: %s", pid)
		}

		if t := cs[0].State.Terminated; t != nil {
			if err := p.ProcessStop(app, pid); err != nil {
				return 0, err
			}

			return int(t.ExitCode), nil
		}
	}
}

func (p *Provider) podSpecFromService(app, service, release string) (*ac.PodSpec, error) {
	if release == "" {
		a, err := p.AppGet(app)
		if err != nil {
			return nil, err
		}

		release = a.Release
	}

	c := ac.Container{
		Env:           []ac.EnvVar{},
		Name:          "main",
		VolumeDevices: []ac.VolumeDevice{},
		VolumeMounts:  []ac.VolumeMount{},
	}

	vs := []ac.Volume{}

	c.VolumeMounts = append(c.VolumeMounts, ac.VolumeMount{
		Name:      "ca",
		MountPath: "/etc/convox",
	})

	vs = append(vs, ac.Volume{
		Name: "ca",
		VolumeSource: ac.VolumeSource{
			ConfigMap: &ac.ConfigMapVolumeSource{
				LocalObjectReference: ac.LocalObjectReference{
					Name: "ca",
				},
				Optional: options.Bool(true),
			},
		},
	})

	if release != "" {
		m, r, err := common.ReleaseManifest(p, app, release)
		if err != nil {
			return nil, err
		}

		env := map[string]string{}

		senv, err := p.systemEnvironment(app, release)
		if err != nil {
			return nil, err
		}

		for k, v := range senv {
			env[k] = v
		}

		e := structs.Environment{}

		if err := e.Load([]byte(r.Env)); err != nil {
			return nil, err
		}

		for k, v := range e {
			env[k] = v
		}

		if s, _ := m.Service(service); s != nil {
			if s.Command != "" {
				parts, err := shellquote.Split(s.Command)
				if err != nil {
					return nil, err
				}
				c.Args = parts
			}

			for k, v := range s.EnvironmentDefaults() {
				env[k] = v
			}

			for _, l := range s.Links {
				env[fmt.Sprintf("%s_URL", envName(l))] = fmt.Sprintf("https://%s.%s.%s", l, app, p.Name)
			}

			for _, r := range s.Resources {
				cm, err := p.Cluster.CoreV1().ConfigMaps(p.AppNamespace(app)).Get(fmt.Sprintf("resource-%s", r), am.GetOptions{})
				if err != nil {
					return nil, err
				}

				env[fmt.Sprintf("%s_URL", envName(r))] = cm.Data["URL"]
			}

			repo, _, err := p.Engine.RepositoryHost(app)
			if err != nil {
				return nil, err
			}

			c.Image = fmt.Sprintf("%s:%s.%s", repo, service, r.Build)

			for _, v := range p.volumeSources(app, s.Name, s.Volumes) {
				vs = append(vs, p.podVolume(app, v))
			}

			for _, v := range s.Volumes {
				to, err := volumeTo(v)
				if err != nil {
					return nil, err
				}

				c.VolumeMounts = append(c.VolumeMounts, ac.VolumeMount{
					Name:      p.volumeName(app, p.volumeFrom(app, s.Name, v)),
					MountPath: to,
				})
			}
		}

		for k, v := range env {
			c.Env = append(c.Env, ac.EnvVar{Name: k, Value: v})
		}
	}

	ps := &ac.PodSpec{
		Containers:            []ac.Container{c},
		ShareProcessNamespace: options.Bool(true),
		Volumes:               vs,
	}

	if ip, err := p.Engine.Resolver(); err == nil {
		ps.DNSPolicy = "None"
		ps.DNSConfig = &ac.PodDNSConfig{
			Nameservers: []string{ip},
			Options: []ac.PodDNSConfigOption{
				{Name: "ndots", Value: options.String("1")},
			},
		}
	}

	return ps, nil
}

func (p *Provider) podSpecFromRunOptions(app, service string, opts structs.ProcessRunOptions) (*ac.PodSpec, error) {
	s, err := p.podSpecFromService(app, service, common.DefaultString(opts.Release, ""))
	if err != nil {
		return nil, err
	}

	if opts.Command != nil {
		parts, err := shellquote.Split(*opts.Command)
		if err != nil {
			return nil, err
		}
		s.Containers[0].Args = parts
	}

	if opts.Environment != nil {
		for k, v := range opts.Environment {
			s.Containers[0].Env = append(s.Containers[0].Env, ac.EnvVar{Name: k, Value: v})
		}
	}

	if opts.Image != nil {
		s.Containers[0].Image = *opts.Image
	}

	if opts.Volumes != nil {
		vs := []string{}

		for from, to := range opts.Volumes {
			vs = append(vs, fmt.Sprintf("%s:%s", from, to))
		}

		for _, v := range p.volumeSources(app, service, vs) {
			s.Volumes = append(s.Volumes, p.podVolume(app, v))
		}

		for _, v := range vs {
			to, err := volumeTo(v)
			if err != nil {
				return nil, err
			}

			s.Containers[0].VolumeMounts = append(s.Containers[0].VolumeMounts, ac.VolumeMount{
				Name:      p.volumeName(app, p.volumeFrom(app, service, v)),
				MountPath: to,
			})
		}
	}

	s.RestartPolicy = "Never"

	return s, nil
}

func (p *Provider) podVolume(app, from string) ac.Volume {
	v := ac.Volume{
		Name: p.volumeName(app, from),
		VolumeSource: ac.VolumeSource{
			PersistentVolumeClaim: &ac.PersistentVolumeClaimVolumeSource{
				ClaimName: p.volumeName(app, from),
			},
		},
	}

	if systemVolume(from) {
		v.VolumeSource = ac.VolumeSource{
			HostPath: &ac.HostPathVolumeSource{
				Path: from,
			},
		}
	}

	return v
}

func processFromPod(pd ac.Pod) (*structs.Process, error) {
	cs := pd.Spec.Containers

	if len(cs) != 1 || cs[0].Name != "main" {
		return nil, fmt.Errorf("unexpected containers for pid: %s", pd.ObjectMeta.Name)
	}

	status := "unknown"

	switch pd.Status.Phase {
	case "Failed":
		status = "failed"
	case "Pending":
		status = "pending"
	case "Running":
		status = "running"
	case "Succeeded":
		status = "complete"
	}

	if cds := pd.Status.Conditions; len(cds) > 0 && status != "complete" && status != "failed" {
		for _, cd := range cds {
			if cd.Type == "Ready" && cd.Status == "False" {
				status = "unhealthy"
			}
		}
	}

	if css := pd.Status.ContainerStatuses; len(css) > 0 && css[0].Name == "main" {
		if cs := css[0]; cs.State.Waiting != nil {
			switch cs.State.Waiting.Reason {
			case "CrashLoopBackOff":
				status = "crashed"
			}
		}
	}

	ps := &structs.Process{
		Id:       pd.ObjectMeta.Name,
		App:      pd.ObjectMeta.Labels["app"],
		Command:  shellquote.Join(cs[0].Args...),
		Host:     "",
		Image:    cs[0].Image,
		Instance: "",
		Name:     pd.ObjectMeta.Labels["service"],
		Release:  pd.ObjectMeta.Labels["release"],
		Started:  pd.CreationTimestamp.Time,
		Status:   status,
	}

	return ps, nil
}

type terminalSize struct {
	Height int
	Width  int
	sent   bool
}

func (ts *terminalSize) Next() *remotecommand.TerminalSize {
	if ts.sent {
		return nil
	}

	ts.sent = true

	return &remotecommand.TerminalSize{Height: uint16(ts.Height), Width: uint16(ts.Width)}
}
