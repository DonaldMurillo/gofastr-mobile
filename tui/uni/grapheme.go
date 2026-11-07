package uni

import "unicode/utf8"

// graphemeLen returns the byte length of the first extended grapheme
// cluster in s, which must not be empty. It applies the rules of UAX #29
// (GB3 to GB999) while scanning forward from the start of the cluster.
func graphemeLen(s string) int {
	r, n := utf8.DecodeRuneInString(s)
	prev := lookup(r)
	var st clusterState
	st.accept(prev)
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		cur := lookup(r)
		if st.breaks(prev, cur) {
			break
		}
		st.accept(cur)
		prev = cur
		n += size
	}
	return n
}

// clusterState carries the context that rules GB9c, GB11 and GB12/13 need
// beyond the pair of code points being compared.
type clusterState struct {
	// pictograph: the cluster so far ends in ExtPict Extend*.
	pictograph bool
	// pictographZWJ: it ends in ExtPict Extend* ZWJ.
	pictographZWJ bool
	// conjunct: 0 outside a conjunct; 1 after InCB=Consonant followed only
	// by InCB=Extend; 2 once an InCB=Linker has been seen in that run.
	conjunct int
	// regional counts the regional indicators at the end of the cluster.
	regional int
}

func (st *clusterState) accept(p prop) {
	switch {
	case p.extPict():
		st.pictograph, st.pictographZWJ = true, false
	case p.gcb() == gcbExtend && st.pictograph:
	case p.gcb() == gcbZWJ && st.pictograph:
		st.pictograph, st.pictographZWJ = false, true
	default:
		st.pictograph, st.pictographZWJ = false, false
	}

	switch c := p.incb(); {
	case c == incbConsonant:
		st.conjunct = 1
	case c == incbLinker && st.conjunct > 0:
		st.conjunct = 2
	case c == incbExtend && st.conjunct > 0:
	default:
		st.conjunct = 0
	}

	if p.gcb() == gcbRegional {
		st.regional++
	} else {
		st.regional = 0
	}
}

func (st *clusterState) breaks(prev, cur prop) bool {
	a, b := prev.gcb(), cur.gcb()
	switch {
	case a == gcbCR && b == gcbLF: // GB3
		return false
	case prev.isControl(), cur.isControl(): // GB4, GB5
		return true
	case a == gcbL && (b == gcbL || b == gcbV || b == gcbLV || b == gcbLVT): // GB6
		return false
	case (a == gcbLV || a == gcbV) && (b == gcbV || b == gcbT): // GB7
		return false
	case (a == gcbLVT || a == gcbT) && b == gcbT: // GB8
		return false
	case b == gcbExtend || b == gcbZWJ: // GB9
		return false
	case b == gcbSpacingMark: // GB9a
		return false
	case a == gcbPrepend: // GB9b
		return false
	case st.conjunct == 2 && cur.incb() == incbConsonant: // GB9c
		return false
	case a == gcbZWJ && st.pictographZWJ && cur.extPict(): // GB11
		return false
	case a == gcbRegional && b == gcbRegional && st.regional%2 == 1: // GB12, GB13
		return false
	}
	return true // GB999
}

// Joins reports whether next, written right after the cluster prev, would
// continue prev's cluster instead of starting its own. Two lone regional
// indicators join into a flag, for example. Empty strings never join.
func Joins(prev, next string) bool {
	if prev == "" || next == "" || next[0] < utf8.RuneSelf && prev[len(prev)-1] < utf8.RuneSelf {
		return false
	}
	var st clusterState
	var last prop
	for _, r := range prev {
		last = lookup(r)
		st.accept(last)
	}
	r, _ := utf8.DecodeRuneInString(next)
	return !st.breaks(last, lookup(r))
}
