job "postgres" {
  datacenters = ["us-east-1c"]
  type        = "service"
  node_pool   = "api"

  group "postgres" {
    count = 1

    network {
      port "db" {
        static = 5432
      }
    }

    service {
      name     = "postgres"
      port     = "db"
      provider = "consul"

      check {
        type     = "tcp"
        port     = "db"
        interval = "10s"
        timeout  = "2s"
      }
    }

    task "postgres" {
      driver = "docker"

      config {
        image = "postgres:15-alpine"
        ports = ["db"]

        # Mount EBS volume subdirectory to avoid ext4 lost+found conflict.
        # The host path /opt/e2b-postgres-data is the EBS mount point;
        # we use /opt/e2b-postgres-data/pgdata as the actual PGDATA dir.
        volumes = [
          "/opt/e2b-postgres-data/pgdata:/var/lib/postgresql/data",
        ]
      }

      env {
        POSTGRES_USER     = "postgres"
        POSTGRES_PASSWORD = "e2b-postgres-pw"
        POSTGRES_DB       = "e2b"
        # Explicitly set PGDATA to match the mounted subdirectory
        PGDATA = "/var/lib/postgresql/data"
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }
  }
}
