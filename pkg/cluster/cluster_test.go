package cluster

import (
	"context"
	"testing"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/stretchr/testify/require"

	//log "github.com/sirupsen/logrus"
)

func TestNFTables(t *testing.T) {
	ctx := context.Background()
	_, clients, err := config.New(ctx)

	cmd := `nft flush ruleset`
	err = runCommandInNodePods(ctx, clients, cmd)
	require.NoError(t, err)
}
