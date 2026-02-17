{
  description = "agentd - Agent daemon";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    {
      overlays.default = final: prev: {
        agentd = final.buildGoModule {
          pname = "agentd";
          version = self.shortRev or "dev";
          src = final.lib.cleanSource self;
          vendorHash = null;
          ldflags = [ "-s" "-w" ];
        };
      };

      # NixOS module systemd service definition
      nixosModules.default = { config, lib, pkgs, ... }:
        let cfg = config.services.agentd; in
        {
          options.services.agentd = {
            enable = lib.mkEnableOption "agentd daemon";
            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
              description = "The agentd package to use";
            };
            extraArgs = lib.mkOption {
              type = lib.types.listOf lib.types.str;
              default = [];
              description = "Additional command-line arguments";
            };
          };

          config = lib.mkIf cfg.enable {
            systemd.services.agentd = {
              description = "Agent Daemon";
              wantedBy = [ "multi-user.target" ];
              after = [ "network.target" ];

              # agentd shells out to tmux for agent session management and
              # uses sudo to run tmux as the agent user (tmux enforces UID
              # ownership on its socket, so sessions must be owned by agent).
              path = [ pkgs.tmux pkgs.sudo ];

              serviceConfig = {
                ExecStart = "${cfg.package}/bin/agentd ${lib.escapeShellArgs cfg.extraArgs}";
                Restart = "always";
                DynamicUser = true;
                StateDirectory = "agentd";
              };
            };
          };
        };
    }
    //
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ self.overlays.default ];
        };
      in
      {
        packages = {
          default = pkgs.agentd;
          agentd = pkgs.agentd;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            # Go toolchain
            go_1_25
            gotools

            # agentd testing requires tmux
            tmux

            # Build tools
            gnumake

            # Test tools
            hurl
          ];

          shellHook = ''
            echo "agentd development environment"
            echo ""
            echo "Go version: $(go version)"
            echo ""
            echo "Available make targets:"
            make help
          '';
        };
      }
    );
}
