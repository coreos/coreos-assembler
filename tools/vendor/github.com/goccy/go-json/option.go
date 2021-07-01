package json

type EncodeOptionFunc func(EncodeOption) EncodeOption

func UnorderedMap() func(EncodeOption) EncodeOption {
	return func(opt EncodeOption) EncodeOption {
		return opt | EncodeOptionUnorderedMap
	}
}
