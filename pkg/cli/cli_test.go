package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/convox/convox/pkg/cli"
	"github.com/convox/convox/pkg/common"
	mocksdk "github.com/convox/convox/pkg/mock/sdk"
	"github.com/convox/convox/pkg/structs"
	shellquote "github.com/kballard/go-shellquote"
	"github.com/stretchr/testify/require"
)

var (
	fxObject = structs.Object{
		Url: "object://test",
	}
)

var (
	fxStarted = time.Now().UTC().Add(-48 * time.Hour)
)

func testClient(t *testing.T, fn func(*cli.Engine, *mocksdk.Interface)) {
	testClientWait(t, 1, fn)
}

func testClientWait(t *testing.T, wait time.Duration, fn func(*cli.Engine, *mocksdk.Interface)) {
	os.Unsetenv("CONVOX_HOST")
	os.Unsetenv("CONVOX_PASSWORD")
	os.Unsetenv("CONVOX_RACK")
	os.Unsetenv("RACK_URL")

	i := &mocksdk.Interface{}

	cli.WaitDuration = wait
	common.ProviderWaitDuration = wait

	e := cli.New("convox", "test")

	e.Client = i

	tmp, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	e.Settings = tmp
	// defer os.RemoveAll(tmp)

	fn(e, i)

	i.AssertExpectations(t)
}

func testExecute(e *cli.Engine, cmd string, stdin io.Reader) (*result, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	return testExecuteContext(ctx, e, cmd, stdin)
}

func testExecuteContext(ctx context.Context, e *cli.Engine, cmd string, stdin io.Reader) (*result, error) {
	if stdin == nil {
		stdin = &bytes.Buffer{}
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	e.Reader.Reader = stdin

	e.Writer.Color = false
	e.Writer.Stdout = &stdout
	e.Writer.Stderr = &stderr

	cp, err := shellquote.Split(cmd)
	if err != nil {
		return nil, err
	}

	code := e.ExecuteContext(ctx, cp)

	res := &result{
		Code:   code,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	return res, nil
}

func testLogs(logs []string) io.ReadCloser {
	return ioutil.NopCloser(strings.NewReader(fmt.Sprintf("%s\n", strings.Join(logs, "\n"))))
}

type result struct {
	Code   int
	Stdout string
	Stderr string
}

func (r *result) RequireStderr(t *testing.T, lines []string) {
	stderr := strings.Split(strings.TrimSuffix(r.Stderr, "\n"), "\n")
	require.Equal(t, lines, stderr)
}

func (r *result) RequireStdout(t *testing.T, lines []string) {
	stdout := strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
	require.Equal(t, lines, stdout)
}
