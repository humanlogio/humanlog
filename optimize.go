package humanlog

// moveToFront moves the element at index `i` to the front
// of the slice
func moveToFront[El any](i int, s []El) []El {
	if i == 0 {
		return s
	}
	el := s[i]
	for j := i; j > 0; j-- {
		s[j] = s[j-1]
	}
	s[0] = el
	return s
}

const dynamicReordering = true
