job "client-proxy" {
  datacenters = ["${aws_region}"]
  node_pool = "${node_pool}"
  priority = 80

  group "client-proxy-service" {
    count = ${count}

    restart {
      interval         = "5s"
      attempts         = 1
      delay            = "5s"
      mode             = "delay"
    }

    network {
      port "proxy" {
        static = ${proxy_port}
      }
      port "api" {
        static = ${api_port}
      }
    }

    constraint {
      operator  = "distinct_hosts"
      value     = "true"
    }

    service {
      name = "client-proxy"
      port = "${proxy_port_name}"
      task = "start"

      check {
        type     = "http"
        name     = "health"
        path     = "/health"
        interval = "5s"
        timeout  = "3s"
        port     = ${api_port}
      }
    }

    service {
      name = "edge-api"
      port = "${api_port_name}"
      task = "start"

      check {
        type     = "http"
        name     = "health"
        path     = "/health"
        interval = "5s"
        timeout  = "3s"
        port     = ${api_port}
      }
    }

%{ if update_stanza }
    update {
      max_parallel     = ${update_max_parallel}
      canary           = 1
      min_healthy_time = "10s"
      healthy_deadline = "300s"
      auto_promote     = true
    }
%{ endif }

    task "start" {
      driver       = "docker"
      kill_timeout = "30s"
      kill_signal  = "SIGTERM"

      resources {
        memory_max = ${memory_mb * 2}
        memory     = ${memory_mb}
        cpu        = ${cpu_count * 1000}
      }

      env {
        NODE_ID                      = "$${node.unique.id}"
        ENVIRONMENT                  = "${environment}"
        PROXY_PORT                   = "${proxy_port}"
        API_PORT                     = "${api_port}"
        API_SECRET                   = "${api_secret}"
        ORCHESTRATOR_PORT            = "${orchestrator_port}"
        REDIS_URL                    = "${redis_url}"
        REDIS_CLUSTER_URL            = "${redis_cluster_url}"
        REDIS_TLS_CA_BASE64          = "${redis_tls_ca_base64}"
        LOKI_URL                     = "${loki_url}"
        NOMAD_ENDPOINT               = "${nomad_endpoint}"
        NOMAD_TOKEN                  = "${nomad_token}"
        OTEL_COLLECTOR_GRPC_ENDPOINT = "${otel_collector_grpc_endpoint}"
        LOGS_COLLECTOR_ADDRESS       = "${logs_collector_address}"

        # AWS-specific
        AWS_REGION                   = "${aws_region}"

%{ if launch_darkly_api_key != "" }
        LAUNCH_DARKLY_API_KEY        = "${launch_darkly_api_key}"
%{ endif }
      }

      config {
        network_mode = "host"
        image        = "${image_name}"
        ports        = ["${proxy_port_name}", "${api_port_name}"]
      }
    }
  }
}
