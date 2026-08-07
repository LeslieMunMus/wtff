# Homebrew formula for wtff.
#
# This builds from source rather than downloading a prebuilt binary, and that
# is the whole point of distributing this way. A binary downloaded from the
# internet is quarantined by Gatekeeper and refuses to open until it is either
# notarized, which needs a paid Apple Developer account, or cleared by hand
# with xattr, which trains people to disarm the exact protection that would
# have caught a genuinely malicious download. Compiling locally sidesteps the
# question honestly: nothing arrives already built, so nothing is quarantined.
#
# Publishing: copy this file to Formula/wtff.rb in a repository named
# homebrew-wtff, then "brew install lesliemunmus/wtff/wtff". Update url and
# sha256 on every release; "make dist" prints the checksum of its archive, and
# the source tarball's checksum comes from
# "curl -sL <url> | shasum -a 256".
class Wtff < Formula
  desc "Terminal-first macOS maintenance toolkit"
  homepage "https://github.com/lesliemunmus/wtff"
  url "https://github.com/lesliemunmus/wtff/archive/refs/tags/v0.2.0.tar.gz"
  sha256 "d1e1285d7dd6061100fd7a173fe7de2bc90f23cb8c46d59c0284ea16ee14e458"
  license "PolyForm-Noncommercial-1.0.0"
  head "https://github.com/lesliemunmus/wtff.git", branch: "main"

  depends_on "go" => :build
  depends_on :macos

  def install
    ldflags = %W[
      -s -w
      -X github.com/lesliemunmus/wtff/internal/cli.Version=#{version}
    ]
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/wtff"
  end

  test do
    # version is the one command that neither needs a terminal nor touches the
    # filesystem, which makes it the only safe thing to run in a sandbox that
    # has no tty and should certainly not be running a cleanup tool.
    assert_match version.to_s, shell_output("#{bin}/wtff version")

    # A dry run against an empty home proves the binary can load its embedded
    # rules and catalog, which is where a broken build would actually show.
    output = shell_output("HOME=#{testpath} #{bin}/wtff clean --dry-run")
    assert_match(/nothing|will stage/, output)
  end
end
