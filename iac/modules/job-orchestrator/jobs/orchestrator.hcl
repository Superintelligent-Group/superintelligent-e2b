job "orchestrator-${latest_orchestrator_job_id}" {
  type = "system"
  node_pool = "${node_pool}"

  priority = 91

  group "client-orchestrator" {
    # Build nodes have a separate role and must never reserve sandbox ports.
    constraint {
      attribute = "$${meta.node_type}"
      operator  = "="
      value     = "worker"
    }
    // For future as we can remove static and allow multiple instances on one machine if needed.
    // Also network allocation is used by Nomad service discovery on API and edge API to find jobs and register them.
    network {
      port "orchestrator" {
        static = "${port}"
      }

      port "orchestrator-proxy" {
        static = "${proxy_port}"
      }
    }

%{ if latest_orchestrator_job_id != "dev" }
    constraint {
      attribute = "$${meta.orchestrator_job_version}"
      value     = "${latest_orchestrator_job_id}"
    }
%{ endif }

    service {
      name = "orchestrator"
      port = "${port}"
      address = "$${attr.unique.network.ip-address}"
      # The task binds a static host-network port. Explicitly publish the
      # node address so Nomad discovery never returns an unroutable blank
      # service address to the API placement loop.
      address_mode = "auto"

      provider = "nomad"

      check {
        type         = "http"
        path         = "/health"
        name         = "health"
        interval     = "20s"
        timeout      = "5s"
      }
    }

    service {
      name = "orchestrator-proxy"
      port = "${proxy_port}"
      address = "$${attr.unique.network.ip-address}"
      address_mode = "auto"

      provider = "nomad"

      check {
        type     = "tcp"
        name     = "health"
        interval = "30s"
        timeout  = "1s"
      }
    }

    task "start" {
      driver = "raw_exec"

      // SUP-676: was attempts=0 (no retries at all) -- unlike every sibling
      // service job (api, redis, client-proxy), which all tolerate a few
      // transient restarts. Confirmed live: on a cold cluster wake, this
      // orchestrator dials Redis (a dependency on a different node pool)
      // within ~1s of process start, sometimes before redis's own
      // allocation has finished binding its port -- a real but transient
      // race, not a persistent failure. With zero retries, that one bad
      // roll of the dice permanently fails the allocation until someone
      // manually forces a new Nomad evaluation. Matches client-proxy's
      // bounded pattern (a few tries, then give up loudly) rather than
      // api/redis's "retry forever" -- this manages live Firecracker VMs,
      // so silently flapping forever on a REAL persistent failure is worse
      // than surfacing it after a bounded number of tries.
      restart {
        attempts = 3
        interval = "5m"
        delay    = "15s"
        mode     = "fail"
      }

      resources {
        # Firecracker's guest RAM is mmap'd by the orchestrator while loading
        # snapshots. Reserve enough cgroup headroom for the guest plus the
        # orchestrator/UFFD process; reserving less than guest RAM fails with
        # "mmap memfd: cannot allocate memory" even when the host has free RAM.
        memory     = ${memory_mb}
        memory_max = -1
      }

      env {
        NODE_ID     = "$${node.unique.name}"
        NODE_IP     = "$${attr.unique.network.ip-address}"
        NODE_LABELS = "$${meta.node_labels}"

        GRPC_PORT                    = "${port}"
        PROXY_PORT                   = "${proxy_port}"

%{ for key, value in job_env_vars ~}
        ${key} = "${value}"
%{ endfor ~}

      }

      config {
        command = "/bin/bash"
        args    = ["-c", " chmod +x local/orchestrator && local/orchestrator"]
      }

      artifact {
        source      = "${artifact_source}"
        destination = "local/orchestrator"
        mode        = "file"
      }
    }
  }
}
