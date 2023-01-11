// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/honestbank/cache-lib-go (interfaces: CacheSubscription)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	redis "github.com/go-redis/redis/v8"
	gomock "github.com/golang/mock/gomock"
	cache_lib "github.com/honestbank/cache-lib-go"
)

// MockCacheSubscription is a mock of CacheSubscription interface.
type MockCacheSubscription struct {
	ctrl     *gomock.Controller
	recorder *MockCacheSubscriptionMockRecorder
}

// MockCacheSubscriptionMockRecorder is the mock recorder for MockCacheSubscription.
type MockCacheSubscriptionMockRecorder struct {
	mock *MockCacheSubscription
}

// NewMockCacheSubscription creates a new mock instance.
func NewMockCacheSubscription(ctrl *gomock.Controller) *MockCacheSubscription {
	mock := &MockCacheSubscription{ctrl: ctrl}
	mock.recorder = &MockCacheSubscriptionMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCacheSubscription) EXPECT() *MockCacheSubscriptionMockRecorder {
	return m.recorder
}

// GetChannel mocks base method.
func (m *MockCacheSubscription) GetChannel(arg0 context.Context) (<-chan *redis.Message, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetChannel", arg0)
	ret0, _ := ret[0].(<-chan *redis.Message)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetChannel indicates an expected call of GetChannel.
func (mr *MockCacheSubscriptionMockRecorder) GetChannel(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetChannel", reflect.TypeOf((*MockCacheSubscription)(nil).GetChannel), arg0)
}

// Subscribe mocks base method.
func (m *MockCacheSubscription) Subscribe(arg0 context.Context) cache_lib.CacheSubscription {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Subscribe", arg0)
	ret0, _ := ret[0].(cache_lib.CacheSubscription)
	return ret0
}

// Subscribe indicates an expected call of Subscribe.
func (mr *MockCacheSubscriptionMockRecorder) Subscribe(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Subscribe", reflect.TypeOf((*MockCacheSubscription)(nil).Subscribe), arg0)
}

// Unsubscribe mocks base method.
func (m *MockCacheSubscription) Unsubscribe(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Unsubscribe", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Unsubscribe indicates an expected call of Unsubscribe.
func (mr *MockCacheSubscriptionMockRecorder) Unsubscribe(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Unsubscribe", reflect.TypeOf((*MockCacheSubscription)(nil).Unsubscribe), arg0)
}
