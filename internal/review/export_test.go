package review

// SetBeforeReanchorWrite runs f between re-anchoring's git work and its write transaction.
func (s *Service) SetBeforeReanchorWrite(f func()) { s.beforeReanchorWrite = f }
