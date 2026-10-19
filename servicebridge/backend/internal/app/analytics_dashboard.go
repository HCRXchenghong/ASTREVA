package app

import "customer-service/backend/internal/store"

// InquiryCounter 暴露底层存储的看板询盘聚合能力。
//
// app.NewService 会用 knowledgeIndexStore 包装存储层（为了让知识库写入自动失效
// 倒排索引），而包装类型只转发 store.Store 接口里的方法。直接对 service.Store()
// 做类型断言会被这层包装挡住，因此这里显式解包到原始存储实现。
// 不支持该能力的自定义 Store 返回 false，调用方应如实显示「未知」而不是 0。
func (s *Service) InquiryCounter() (store.InquiryCounter, bool) {
	if s == nil || s.store == nil {
		return nil, false
	}
	if counter, ok := s.store.(store.InquiryCounter); ok {
		return counter, true
	}
	if wrapper, ok := s.store.(*knowledgeIndexStore); ok && wrapper != nil {
		counter, ok := wrapper.Store.(store.InquiryCounter)
		return counter, ok
	}
	return nil, false
}
