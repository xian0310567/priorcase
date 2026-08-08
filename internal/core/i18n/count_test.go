package i18n

import "testing"

// 영어는 수 일치가 있고 한국어는 없다. `%d segments` 가 1일 때 비문이 되는 것을 막는다.
func TestCountAgreesWithNumber(t *testing.T) {
	for _, tc := range []struct {
		lang Lang
		n    int
		want string
	}{
		{EN, 1, "1 unreviewed segment"},
		{EN, 2, "2 unreviewed segments"},
		{EN, 0, "0 unreviewed segments"},
		{KO, 1, "1건"},
		{KO, 7, "7건"},
	} {
		if got := tc.lang.Count(tc.n, "건", "unreviewed segment", "unreviewed segments"); got != tc.want {
			t.Errorf("%s/%d = %q, want %q", tc.lang, tc.n, got, tc.want)
		}
	}
}
