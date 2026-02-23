# Maintainer: Timothy Thomas <timothy.thomas2011@hotmail.com>
pkgname=fpf-cli
pkgver=1.6.33
pkgrel=1
pkgdesc="Cross-platform fuzzy package finder powered by fzf"
arch=('x86_64' 'aarch64' 'armv7h')
url="https://github.com/Timmy6942025/fpf-cli"
license=('Apache-2.0')
depends=('bash'
    'fzf')
makedepends=('git')
optdepends=()
source=("git+https://github.com/Timmy6942025/fpf-cli.git#tag=v${pkgver}")
md5sums=('SKIP')

package() {
	cd "$srcdir/fpf-cli"
	install -Dm755 fpf -t "$pkgdir/usr/bin"
}
