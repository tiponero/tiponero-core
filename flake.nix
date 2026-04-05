{
  description = "Tiponero - self-hosted Monero donation platform";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      forAllSystems = fn:
        nixpkgs.lib.genAttrs
          [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ]
          (system: fn nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        tiponero = pkgs.buildGoModule {
          pname = "tiponero";
          version = "1.0.0";
          src = pkgs.lib.cleanSource ./.;

          vendorHash = null;

          nativeBuildInputs = with pkgs; [ templ tailwindcss ];

          env.CGO_ENABLED = "1";

          preBuild = ''
            templ generate
            tailwindcss -i static/css/input.css -o static/css/output.css --minify
          '';

          subPackages = [ "cmd/tiponero" ];

          meta = {
            description = "Self-hosted Monero donation platform";
            mainProgram = "tiponero";
          };
        };
        default = tiponero;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            templ
            tailwindcss
            sqlite
            air
            golangci-lint
          ];
          CGO_ENABLED = "1";
        };
      });
    };
}
