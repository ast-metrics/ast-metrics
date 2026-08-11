# Installing AST Metrics

AST Metrics is built in Golang and distributed as a single binary. It has no dependencies.

## 🚀 Quick Install

Choose your preferred method below.

???+ info ":simple-homebrew: Homebrew (Linux/MacOS)"

    ```bash
    brew install ast-metrics/tap/ast-metrics
    ```

    Homebrew keeps the binary up to date: `brew upgrade` installs new releases.

??? info ":magic_wand: Automatic Install (Linux/MacOS/Windows)"

    Run the following command to download the latest version:

    ```bash
    curl -fsSL https://install.ast-metrics.dev|sh
    ```

    Then move the `./ast-metrics` binary to a directory in your `PATH` (e.g. `/usr/local/bin` for Linux/MacOS).

    > Be careful when running scripts from the internet. Always check the content of the script before running it.

??? info ":simple-debian: Debian / Ubuntu (.deb) and Fedora / RHEL (.rpm)"

    Each release ships `.deb` and `.rpm` packages for `amd64` and `arm64`. Download the one for your platform from the [latest release](https://github.com/ast-metrics/ast-metrics/releases/latest), then:

    ```bash
    # Debian / Ubuntu
    sudo dpkg -i ast-metrics_*_amd64.deb

    # Fedora / RHEL
    sudo rpm -i ast-metrics-*.x86_64.rpm
    ```

??? info ":simple-linux: Linux (Manual)"

    Download the binary for your platform (run `uname -m` in your terminal to get your architecture):

    - [amd64](https://github.com/ast-metrics/ast-metrics/releases/latest/download/ast-metrics_Linux_x86_64) (most common)
    - [arm64](https://github.com/ast-metrics/ast-metrics/releases/latest/download/ast-metrics_Linux_arm64) (for Raspberry Pi)

??? info ":simple-apple: MacOS (Manual)"

    Download the binary for your platform (run `uname -m` in your terminal to get your architecture):
    
    - [arm64](https://github.com/ast-metrics/ast-metrics/releases/latest/download/ast-metrics_Darwin_arm64) (for Apple Silicon / M1 / M2)
    - [amd64](https://github.com/ast-metrics/ast-metrics/releases/latest/download/ast-metrics_Darwin_x86_64) (for Intel Macs)

??? info ":fontawesome-brands-windows: Windows (Manual)"

    Download the executable:

    - [amd64](https://github.com/ast-metrics/ast-metrics/releases/latest/download/ast-metrics_Windows_x86_64.exe)

??? info ":simple-docker: Docker"

    No installation at all. Mount your project and run:

    ```bash
    docker run --rm -v $(pwd):/src ghcr.io/ast-metrics/ast-metrics:latest analyze --report-html=/src/report /src
    ```

    The image is published on each release for `amd64` and `arm64`: [ghcr.io/ast-metrics/ast-metrics](https://github.com/ast-metrics/ast-metrics/pkgs/container/ast-metrics).

??? info ":elephant: PHP Project (Composer)"

    If you are working on a PHP project, you can install AST Metrics as a dev dependency via Composer.
    This is the recommended way for PHP developers as it manages the binary version for you.

    ```bash
    composer require --dev ast-metrics/ast-metrics
    ```

    Then you can run it using:

    ```bash
    php vendor/bin/ast-metrics analyze .
    ```

??? info ":simple-go: Go Install"

    If you have Go and a C compiler installed (CGO is required by the tree-sitter parsers):

    ```bash
    go install github.com/ast-metrics/ast-metrics/cmd/ast-metrics@latest
    ```

## Verify Installation

Verify that the installation worked by opening a new terminal session and listing AST Metrics's available subcommands.

```bash
ast-metrics --help
```

You should see the help message with the available subcommands.

## Troubleshooting

If you get an error that the command `ast-metrics` is not found, you may need to add the directory where the binary is located to your PATH.

## Updating

Update is really easy. Just run:

```bash
ast-metrics self-update
```

If you installed AST Metrics with Homebrew, a package manager or Docker, update it the usual way instead (`brew upgrade ast-metrics`, `docker pull`, or the package of the new release).
