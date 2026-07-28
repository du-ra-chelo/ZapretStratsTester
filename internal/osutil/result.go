package osutil

type CgroupResult struct {
	slice string
	unit  string
	out   string
	err   error
}

func (cr CgroupResult) Slice() string { return cr.slice }
func (cr CgroupResult) Unit() string  { return cr.unit }
func (cr CgroupResult) Err() error    { return cr.err }
