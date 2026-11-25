package workdir

import (
	"os"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.RunGitIsolated(m))
}
