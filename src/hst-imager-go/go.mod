module hst-imager-go

go 1.22

require (
	github.com/jeromelesaux/lha v0.0.0-20240215225629-7cf6ac77ab56
	github.com/koron-go/lha v0.0.0-20251124135154-c32ac3febb9c
	github.com/nwaples/rardecode v1.1.3
	github.com/ulikunitz/xz v0.5.15
)

require github.com/google/go-cmp v0.7.0 // indirect

replace github.com/koron-go/lha => ./third_party/koron-lha
