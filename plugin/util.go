package plugin

import (
	"strings"

	"github.com/gertd/go-pluralize"
)

var pluralClient = pluralize.NewClient()

func plural(word string) string {
	return pluralClient.Plural(word)
}

func hasBase(pkgPath, tail string) bool {
	return pkgPath == tail ||
		strings.HasSuffix(pkgPath, tail) && pkgPath[len(pkgPath)-len(tail)-1] == '/'
}

func hasPrefixCamel(s string, prefix string) bool {
	ln := len(prefix)
	return len(s) > ln &&
		s[:ln] == prefix &&
		!(s[ln] >= 'a' && s[ln] <= 'z')
}
