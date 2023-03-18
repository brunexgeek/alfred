package extra

import (
	"fmt"
	"io"
)

func ReadAll(r io.Reader, limit uint32) ([]byte, error) {
	var total uint32 = 0
	buf := make([]byte, 0, 100*1024)
	for {
		if len(buf) == cap(buf) {
			// Add more capacity (let append pick how much).
			buf = append(buf, 0)[:len(buf)]
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		total += uint32(n)
		buf = buf[:len(buf)+n]
		if total > limit {
			return buf, fmt.Errorf("Too much data")
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return buf, err
		}
	}
}
