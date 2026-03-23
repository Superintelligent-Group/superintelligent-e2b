job "template-manager-system" {
  datacenters = ["${aws_region}"]
  type = "system"
  node_pool  = "${node_pool}"
  priority = 70

# https://developer.hashicorp.com/nomad/docs/job-specification/update
%{ if update_stanza }
  update {
    max_parallel      = 1    # Update only 1 node at a time
  }
%{ endif }

  group "template-manager" {

    // Try to restart the task indefinitely
    // Tries to restart every 5 seconds
    restart {
      interval         = "5s"
      attempts         = 1
      delay            = "5s"
      mode             = "delay"
    }

    network {
      port "template-manager" {
        static = "${port}"
      }
    }

    service {
      name     = "template-manager"
      port     = "${port}"
      provider = "nomad"

      check {
        type         = "http"
        path         = "/health"
        name         = "health"
        interval     = "20s"
        timeout      = "5s"
      }
    }

    task "start" {
      driver = "raw_exec"

%{ if update_stanza }
      # https://developer.hashicorp.com/nomad/docs/configuration/client#max_kill_timeout
      kill_timeout      = "70m"
%{ else }
      kill_timeout      = "1m"
%{ endif }
      kill_signal  = "SIGTERM"

      resources {
        memory     = 1024
        cpu        = 256
      }

      env {
        NODE_ID                         = "$${node.unique.name}"
        CONSUL_TOKEN                    = "${consul_acl_token}"
        API_SECRET                      = "${api_secret}"
        OTEL_TRACING_PRINT              = "${otel_tracing_print}"
        ENVIRONMENT                     = "${environment}"
        TEMPLATE_BUCKET_NAME            = "${template_bucket_name}"
        BUILD_CACHE_BUCKET_NAME         = "${build_cache_bucket_name}"
        OTEL_COLLECTOR_GRPC_ENDPOINT    = "${otel_collector_grpc_endpoint}"
        LOGS_COLLECTOR_ADDRESS          = "${logs_collector_address}"
        ORCHESTRATOR_SERVICES           = "${orchestrator_services}"
        SHARED_CHUNK_CACHE_PATH         = "${shared_chunk_cache_path}"
        CLICKHOUSE_CONNECTION_STRING    = "${clickhouse_connection_string}"
        DOCKERHUB_REMOTE_REPOSITORY_URL = "${dockerhub_remote_repository_url}"
        GRPC_PORT                       = "${port}"
        GIN_MODE                        = "release"

        # AWS-specific configuration
        AWS_REGION                      = "${aws_region}"
        AWS_DOCKER_REPOSITORY_NAME      = "${docker_registry}"
        STORAGE_PROVIDER                = "AWSBucket"

%{ if !update_stanza }
        FORCE_STOP                      = "true"
%{ endif }
%{ if launch_darkly_api_key != "" }
        LAUNCH_DARKLY_API_KEY           = "${launch_darkly_api_key}"
%{ endif }
      }

      config {
        command = "/bin/bash"
        args    = ["-c", " chmod +x local/template-manager && local/template-manager"]
      }

      artifact {
        source      = "s3::https://s3-${aws_region}.amazonaws.com/${bucket_name}/template-manager"
        options {
            checksum    = "md5:${template_manager_checksum}"
        }
      }
    }
  }
}
