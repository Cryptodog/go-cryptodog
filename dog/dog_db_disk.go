package dog

import (
	"fmt"

	"github.com/superp00t/etc"
)

type Disk_DB struct {
	base etc.Path
}

func Disk(path string) *Disk_DB {
	ddb := &Disk_DB{
		etc.ParseSystemPath(path),
	}

	if !ddb.base.IsExtant() {
		if err := ddb.base.Mkdir(); err != nil {
			panic(err)
		}
	}

	if !ddb.base.IsDirectory() {
		panic(fmt.Errorf("dog: %s is not a directory", path))
	}

	return ddb
}

func (d *Disk_DB) Store(key, value interface{}) {
	k := key.(string)
	v := value.(string)

	d.base.Concat(k).WriteAll([]byte(v))
}

func (d *Disk_DB) Load(key interface{}) (interface{}, bool) {
	k := key.(string)

	bytes, err := d.base.Concat(k).ReadAll()
	if err != nil {
		return nil, false
	}

	return string(bytes), true
}

func (d *Disk_DB) Delete(key interface{}) {
	d.base.Concat(key.(string)).Remove()
}
