package httpapi_test

import "github.com/zyf2007/ChatAPI/internal/platform/media"

func testMediaProcessor() media.Processor {
	processor, err := media.NewProcessor(media.ProcessorConfig{})
	if err != nil {
		panic("create test image processor: " + err.Error())
	}
	return processor
}
