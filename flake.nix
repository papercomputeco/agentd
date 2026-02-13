{
  description = "agentd - Development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            # Go toolchain
            pkgs.go_1_25
            pkgs.gotools

            # agentd testing requires tmux
            pkgs.tmux

            # Build tools
            pkgs.gnumake

            # Test tools
            pkgs.hurl
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
