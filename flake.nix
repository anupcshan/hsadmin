{
  description = "hsadmin - Web-based admin interface for Headscale";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    {
      # NixOS module for systemd service
      nixosModules.default = { config, lib, pkgs, ... }:
        with lib;
        let
          cfg = config.services.hsadmin;
        in {
          options.services.hsadmin = {
            enable = mkEnableOption "hsadmin web interface for Headscale";

            package = mkOption {
              type = types.package;
              default = self.packages.${pkgs.system}.hsadmin;
              defaultText = literalExpression "self.packages.\${pkgs.system}.hsadmin";
              description = "The hsadmin package to use";
            };

            configFile = mkOption {
              type = types.path;
              description = "Path to the hsadmin configuration file (hsadmin.yaml)";
              example = "/etc/hsadmin/hsadmin.yaml";
            };

            user = mkOption {
              type = types.str;
              default = "hsadmin";
              description = "User account under which hsadmin runs";
            };

            group = mkOption {
              type = types.str;
              default = "hsadmin";
              description = "Group under which hsadmin runs";
            };
          };

          config = mkIf cfg.enable {
            users.users.${cfg.user} = {
              isSystemUser = true;
              group = cfg.group;
              description = "hsadmin daemon user";
            };

            users.groups.${cfg.group} = {};

            systemd.services.hsadmin = {
              description = "hsadmin - Web-based admin interface for Headscale";
              after = [ "network-online.target" ];
              wants = [ "network-online.target" ];
              wantedBy = [ "multi-user.target" ];

              serviceConfig = {
                Type = "simple";
                User = cfg.user;
                Group = cfg.group;
                ExecStart = "${cfg.package}/bin/hsadmin -config ${cfg.configFile}";
                Restart = "on-failure";
                RestartSec = "5s";

                # Security hardening
                NoNewPrivileges = true;
                PrivateTmp = true;
                ProtectSystem = "strict";
                ProtectHome = true;
                ReadWritePaths = [ "/var/lib/hsadmin" ];
                StateDirectory = "hsadmin";
                Environment = [ "HOME=/var/lib/hsadmin" ];
              };
            };
          };
        };
    }
    // flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

        # Extract version from git or use a default
        version = if (builtins.pathExists ./.git)
          then builtins.substring 0 8 (self.rev or "dirty")
          else "dev";

      in
      {
        packages = {
          # Main hsadmin package
          hsadmin = pkgs.buildGoModule {
            pname = "hsadmin";
            inherit version;

            src = ./.;

            # This will need to be updated when dependencies change
            # Run: nix build .#hsadmin 2>&1 | grep "got:" to get the correct hash
            vendorHash = "sha256-b4ic+gz+tZZ85WEoil0gvcDJolnCRpUsxJ9jS9Fuxrw=";

            # Use pure Go implementations
            tags = [ "osusergo" "netgo" ];

            # Build static binary
            env.CGO_ENABLED = "0";

            # Embed templates into the binary
            preBuild = ''
              # Templates are embedded via go:embed, no additional steps needed
            '';

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
              "-extldflags=-static"
            ];

            # Skip tests during build (they require Docker for integration tests)
            doCheck = false;

            meta = with pkgs.lib; {
              description = "Web-based admin interface for Headscale";
              homepage = "https://github.com/anupcshan/hsadmin";
              license = licenses.mit;
              maintainers = [ ];
            };
          };

          default = self.packages.${system}.hsadmin;
        };

        # Development shell
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            # Go toolchain (1.25.x required)
            go_1_25

            # Required for integration tests
            docker
            docker-compose

            # Version control
            git
          ];

          shellHook = ''
            echo "hsadmin development environment"
            echo "================================"
            echo "Go version: $(go version)"
            echo "golangci-lint version: $(golangci-lint version --format short 2>/dev/null || echo 'not available')"
            echo ""
            echo "Available commands:"
            echo "  go run main.go -config hsadmin.yaml    # Run the application"
            echo "  go test -short ./...                   # Run unit tests"
            echo "  go test ./test/integration             # Run integration tests (requires Docker)"
            echo "  go build -o hsadmin main.go            # Build binary"
            echo "  golangci-lint run                      # Run linter"
            echo ""
            echo "Note: Integration tests require Docker daemon to be running"
          '';
        };

        # Apps for running hsadmin directly
        apps.default = {
          type = "app";
          program = "${self.packages.${system}.hsadmin}/bin/hsadmin";
        };

        # Formatting check
        checks = {
          formatting = pkgs.runCommand "check-formatting" {
            buildInputs = [ pkgs.go_1_25 ];
          } ''
            cd ${./.}
            ${pkgs.go_1_25}/bin/go fmt ./...
            mkdir $out
          '';
        };
      }
    );
}
