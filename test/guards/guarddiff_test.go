// tools/guarddiff runs the T59 matrix from corpus/matrix.tsv: each of its
// rows is matrixTable's row of the same name, the same tool and input with
// the project written <P> and the framework <FW>.

package guards

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestGuarddiffMatrixRows(t *testing.T) {
	t.Parallel()
	rows := map[string][2]string{}
	for _, l := range strings.Split(support.Read(t, "corpus/matrix.tsv"), "\n") {
		col := strings.Split(l, "\t")
		if strings.HasPrefix(l, "#") || len(col) != 6 {
			continue
		}
		input, err := strconv.Unquote(col[4])
		if err != nil {
			t.Fatalf("matrix.tsv: %q: %v", l, err)
		}
		rows[col[5]] = [2]string{col[3], input}
	}
	f := matrixFixture{dir: "<P>", fw: "<FW>"}
	for _, row := range matrixTable {
		tool, input := row.call(f)
		want := input["command"]
		if tool != "Bash" {
			want = js(t, input)
		}
		if got := rows[row.name]; got != [2]string{tool, want.(string)} {
			t.Errorf("matrix.tsv's %s is %q, matrixTable's is %s %q", row.name, got, tool, want)
		}
		delete(rows, row.name)
	}
	for name := range rows {
		t.Errorf("matrix.tsv's %s is no row of matrixTable", name)
	}
}
