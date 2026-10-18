/*
Copyright 2026 operator-testbench contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ydixken/operator-testbench/internal/testbench"
)

// shardRolesPoll is how often valkeyShardRoles asks the pods again.
const shardRolesPoll = 5 * time.Second

// valkeyShardRoles finds the live primary and the in-sync replica of one shard
// by asking Valkey itself: ValkeyNode status.role lags a failover, and the
// node-index labels never move. Args: cluster (the ValkeyCluster name) and
// shard (the valkey.io/shard-index label value). It returns primary and
// replica, the replica empty when the shard has one pod, and polls until a
// primary and, for a shard with several pods, a replica with its link up are
// found. Errors name pods, never addresses.
func valkeyShardRoles(ctx context.Context, r *testbench.Run, args map[string]string) (map[string]string, error) {
	cluster, shard := args["cluster"], args["shard"]
	if cluster == "" || shard == "" {
		return nil, errors.New("valkey.shardRoles needs the args cluster and shard")
	}
	selector := client.MatchingLabels{"valkey.io/cluster": cluster, "valkey.io/shard-index": shard}
	for {
		roles, err := shardRolesOnce(ctx, r, selector)
		if err == nil {
			return roles, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (last: %v)", ctx.Err(), err)
		case <-time.After(shardRolesPoll):
		}
	}
}

func shardRolesOnce(ctx context.Context, r *testbench.Run, selector client.MatchingLabels) (map[string]string, error) {
	var pods corev1.PodList
	if err := r.Client.List(ctx, &pods, client.InNamespace(r.Namespace), selector); err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pod matches %v", selector)
	}
	slices.SortFunc(pods.Items, func(a, b corev1.Pod) int { return strings.Compare(a.Name, b.Name) })
	roles := map[string]string{"primary": "", "replica": ""}
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		out, err := r.ExecOutput(ctx, p.Name, "server", "sh", "-c", "unset VALKEYCLI_AUTH REDISCLI_AUTH; valkey-cli INFO replication")
		if err != nil {
			return nil, err
		}
		info := string(out)
		switch {
		case strings.Contains(info, "role:master"):
			roles["primary"] = p.Name
		case strings.Contains(info, "role:slave") && strings.Contains(info, "master_link_status:up"):
			roles["replica"] = p.Name
		}
	}
	if roles["primary"] == "" {
		return nil, fmt.Errorf("no pod of the shard reports role:master yet (%d pods)", len(pods.Items))
	}
	if len(pods.Items) > 1 && roles["replica"] == "" {
		return nil, fmt.Errorf("no replica of the shard has its link up yet (%d pods)", len(pods.Items))
	}
	return roles, nil
}
