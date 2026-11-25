package review

// SetBeforeReanchorWrite runs f between re-anchoring's git work and its write transaction.
func (s *Service) SetBeforeReanchorWrite(f func()) { s.beforeReanchorWrite = f }

// SetBeforeThreadWrite runs f between placing a new thread and its write transaction.
func (s *Service) SetBeforeThreadWrite(f func()) { s.beforeThreadWrite = f }

// Files are where git keeps the refs read for t.
func (t Tips) Files() []string { return t.files }
