# Maintainer: Eric Lay <ericlaytm@gmail.com>
# Contributor: Yochananmarqos
pkgname=fuzzy-pkg-finder
pkgver=1.2
pkgrel=1
pkgdesc="Cross-platform fuzzy package finder powered by fzf"
arch=('x86_64' 'aarch64' 'armv7h')
url="https://github.com/Timmy6942025/fpf-cli"
license=('Apache')
depends=('bash'
    'fzf'
    'pacman')
makedepends=('git')
optdepends=()
source=("git+https://github.com/Timmy6942025/fpf-cli.git#branch=master")
md5sums=('SKIP')

package() {
	cd "$srcdir/$pkgname"
	install -Dm755 fpf -t "$pkgdir/usr/bin"
}
