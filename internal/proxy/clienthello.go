// Package proxy is a tiny local HTTP CONNECT proxy that gets past DNS poisoning and SNI-based blocking:
// it looks host names up over DNS-over-HTTPS and splits the TLS ClientHello into several TLS records.
// It never decrypts anything.
package proxy

// sniRange returns the offset and length of the SNI host name inside a TLS handshake record *payload*
// (the bytes after the 5-byte record header).
func sniRange(p []byte) (off, n int, ok bool) {
	i := 0
	u8 := func() (int, bool) {
		if i >= len(p) {
			return 0, false
		}
		v := int(p[i])
		i++
		return v, true
	}
	u16 := func() (int, bool) {
		if i+1 >= len(p) {
			return 0, false
		}
		v := int(p[i])<<8 | int(p[i+1])
		i += 2
		return v, true
	}
	if t, ok := u8(); !ok || t != 0x01 { // handshake type: ClientHello
		return 0, 0, false
	}
	i += 3        // handshake length
	i += 2 + 32   // client version + random
	l, ok := u8() // session id
	if !ok {
		return 0, 0, false
	}
	i += l
	if l, ok = u16(); !ok { // cipher suites
		return 0, 0, false
	}
	i += l
	if l, ok = u8(); !ok { // compression methods
		return 0, 0, false
	}
	i += l
	total, ok := u16() // extensions
	if !ok {
		return 0, 0, false
	}
	end := i + total
	if end > len(p) {
		end = len(p)
	}
	for i+4 <= end {
		typ, _ := u16()
		l, _ = u16()
		if typ == 0x0000 { // server_name
			j := i + 2 // skip server_name_list length
			if j+3 > len(p) || p[j] != 0 {
				return 0, 0, false
			}
			nl := int(p[j+1])<<8 | int(p[j+2])
			j += 3
			if nl <= 0 || j+nl > len(p) {
				return 0, 0, false
			}
			return j, nl, true
		}
		i += l
	}
	return 0, 0, false
}

// Fragment splits one complete TLS handshake record (5-byte header + payload) into several records:
// one cut just before the host name and one in the middle of it, so no single record holds the whole name.
// It returns nil when the input is not a splittable handshake record.
func Fragment(rec []byte) [][]byte {
	if len(rec) <= 5 || rec[0] != 0x16 {
		return nil
	}
	payload := rec[5:]
	var cuts []int
	if off, n, ok := sniRange(payload); ok && n >= 2 {
		cuts = []int{off, off + n/2}
	} else {
		cuts = []int{len(payload) / 2}
	}
	cuts = append(cuts, len(payload))
	var out [][]byte
	start := 0
	for _, c := range cuts {
		if c <= start || c > len(payload) {
			continue
		}
		piece := payload[start:c]
		r := make([]byte, 0, 5+len(piece))
		r = append(r, rec[0], rec[1], rec[2], byte(len(piece)>>8), byte(len(piece)))
		r = append(r, piece...)
		out = append(out, r)
		start = c
	}
	if len(out) < 2 {
		return nil
	}
	return out
}
