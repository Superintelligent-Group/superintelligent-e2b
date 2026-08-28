package clusters

import (
	"testing"

	"github.com/e2b-dev/infra/packages/shared/pkg/consts"
	"github.com/e2b-dev/infra/packages/shared/pkg/smap"
	"github.com/google/uuid"
)

func TestLocalClusterUsesConfiguredSandboxDomain(t *testing.T) {
	cluster := localClusterConfig("e2b.example.test")
	if cluster.SandboxProxyDomain == nil || *cluster.SandboxProxyDomain != "e2b.example.test" {
		t.Fatalf("local cluster domain = %v, want configured domain", cluster.SandboxProxyDomain)
	}

	if localClusterConfig(" ").SandboxProxyDomain != nil {
		t.Fatal("blank domain must remain unset")
	}
}

func TestGetSandboxDomainFallsBackToLocalCluster(t *testing.T) {
	domain := "e2b.example.test"
	p := &Pool{clusters: smap.New[*Cluster]()}
	p.clusters.Insert(consts.LocalClusterID.String(), &Cluster{ID: consts.LocalClusterID, SandboxDomain: &domain})

	if got, ok := p.GetSandboxDomain(nil); !ok || got == nil || *got != domain {
		t.Fatalf("fallback domain = %v, want %q", got, domain)
	}

	remoteID := uuid.New()
	remoteDomain := "remote.example.test"
	p.clusters.Insert(remoteID.String(), &Cluster{ID: remoteID, SandboxDomain: &remoteDomain})
	if got, ok := p.GetSandboxDomain(&remoteID); !ok || got == nil || *got != remoteDomain {
		t.Fatalf("remote domain = %v, want %q", got, remoteDomain)
	}

	missingID := uuid.New()
	if got, ok := p.GetSandboxDomain(&missingID); ok || got != nil {
		t.Fatalf("missing explicit cluster resolved to %v, want fail-closed", got)
	}

}

func TestGetSandboxDomainUsesConfiguredDomainBeforeFirstSync(t *testing.T) {
	domain := "e2b.example.test"
	p := &Pool{clusters: smap.New[*Cluster](), localSandboxDomain: &domain}

	if got, ok := p.GetSandboxDomain(nil); !ok || got == nil || *got != domain {
		t.Fatalf("pre-sync fallback = %v, want %q", got, domain)
	}
	if got, ok := p.GetSandboxDomain(&consts.LocalClusterID); !ok || got == nil || *got != domain {
		t.Fatalf("explicit local pre-sync fallback = %v, want %q", got, domain)
	}
}
