package core

import (
	"github.com/sydnash/lotou/timer"
)

type Module interface {
	//OnInit is is the first call in service's goroutinue.
	OnInit()
	//OnDestory is called when service is closed
	OnDestroy()
	//OnMainLoop is called ever main loop, the delta time is specific by GetDuration()
	OnMainLoop(dt int) //dt is the duration time(unit Millisecond)
	//OnNormalMSG is called when received msg from Send() or RawSend() with MSG_TYPE_NORMAL
	OnNormalMSG(msg *Message)
	//OnSocketMSG is called when received msg from Send() or RawSend() with MSG_TYPE_SOCKET
	OnSocketMSG(msg *Message)
	//OnRequestMSG is called when received msg from Request()
	OnRequestMSG(msg *Message)
	//OnCallMSG is called when received msg from Call()
	OnCallMSG(msg *Message)
	//OnDistributeMSG is called when received msg from Send() or RawSend() with MSG_TYPE_DISTRIBUTE
	OnDistributeMSG(msg *Message)
	//OnCloseNotify is called when received msg from SendClose() with false param.
	OnCloseNotify()

	setService(s *service)
	getDuration() int
}

type Skeleton struct {
	s                 *service
	Id                ServiceID
	Name              string
	D                 int
	normalDispatcher  *CallHelper
	requestDispatcher *CallHelper
	callDispatcher    *CallHelper
}

func NewSkeleton(d int) *Skeleton {
	ret := &Skeleton{D: d}
	ret.normalDispatcher = NewCallHelper()
	ret.requestDispatcher = NewCallHelper()
	ret.callDispatcher = NewCallHelper()
	return ret
}

func (s *Skeleton) setService(ser *service) {
	s.s = ser
	s.Id = ser.getId()
	s.Name = ser.getName()
}

func (s *Skeleton) getDuration() int {
	return s.D
}

//use gob encode(not golang's standard library, see "github.com/sydnash/lotou/encoding/gob"
//only support basic types and Message
//user defined struct should encode and decode by user
func (s *Skeleton) Send(dst ServiceID, msgType, encType int32, methodId interface{}, data ...interface{}) {
	send(s.s.getId(), dst, msgType, encType, 0, methodId, data...)
}

//RawSend not encode variables, be careful use
//variables that passed by reference may be changed by others
func (s *Skeleton) RawSend(dst ServiceID, msgType int32, methodId interface{}, data ...interface{}) {
	sendNoEnc(s.s.getId(), dst, msgType, 0, methodId, data...)
}

//if isForce is false, then it will just notify the sevice it need to close
//then service can do choose close immediate or close after self clean.
//if isForce is true, then it close immediate
func (s *Skeleton) SendClose(dst ServiceID, isForce bool) {
	sendNoEnc(s.s.getId(), dst, MSG_TYPE_CLOSE, 0, 0, isForce)
}

//Request send a request msg to dst, and start timeout function if timeout > 0
//after receiver call Respond, the responseCb will be called
func (s *Skeleton) Request(dst ServiceID, encType int32, timeout int, responseCb interface{}, methodId interface{}, data ...interface{}) {
	s.s.request(dst, encType, timeout, responseCb, methodId, data...)
}

//Respond used to respond request msg
func (s *Skeleton) Respond(dst ServiceID, encType int32, rid uint64, data ...interface{}) {
	s.s.respond(dst, encType, rid, data...)
}

//Call send a call msg to dst, and start a timeout function with the conf.CallTimeOut
//after receiver call Ret, it will return
func (s *Skeleton) Call(dst ServiceID, encType int32, methodId interface{}, data ...interface{}) ([]interface{}, error) {
	return s.s.call(dst, encType, methodId, data...)
}

func (s *Skeleton) Schedule(interval, repeat int, cb timer.TimerCallback) *timer.Timer {
	if s.s == nil {
		panic("Schedule must call after OnInit is called(contain OnInit)")
	}
	return s.s.schedule(interval, repeat, cb)
}

//Ret used to ret call msg
func (s *Skeleton) Ret(dst ServiceID, encType int32, cid uint64, data ...interface{}) {
	s.s.ret(dst, encType, cid, data...)
}

func (s *Skeleton) OnDestroy() {
}
func (s *Skeleton) OnMainLoop(dt int) {
}
func (s *Skeleton) OnNormalMSG(msg *Message) {
	s.normalDispatcher.Call(msg.MethodId, msg.Src, msg.Data...)
}
func (s *Skeleton) OnInit() {
}
func (s *Skeleton) OnSocketMSG(msg *Message) {
}
func (s *Skeleton) OnRequestMSG(msg *Message) {
	isAutoReply := s.requestDispatcher.getIsAutoReply(msg.MethodId)
	if isAutoReply {
		ret := s.requestDispatcher.Call(msg.MethodId, msg.Src, msg.Data...)
		s.Respond(msg.Src, msg.EncType, msg.Id, ret...)
	} else {
		s.requestDispatcher.CallWithReplyFunc(msg.MethodId, msg.Src, func(ret ...interface{}) {
			s.Respond(msg.Src, msg.EncType, msg.Id, ret...)
		}, msg.Data...)
	}
}
func (s *Skeleton) OnCallMSG(msg *Message) {
	isAutoReply := s.callDispatcher.getIsAutoReply(msg.MethodId)
	if isAutoReply {
		ret := s.callDispatcher.Call(msg.MethodId, msg.Src, msg.Data...)
		s.Ret(msg.Src, msg.EncType, msg.Id, ret...)
	} else {
		s.callDispatcher.CallWithReplyFunc(msg.MethodId, msg.Src, func(ret ...interface{}) {
			s.Ret(msg.Src, msg.EncType, msg.Id, ret...)
		}, msg.Data...)
	}
}

func (s *Skeleton) findCallerByType(msgType int32) *CallHelper {
	var caller *CallHelper
	switch msgType {
	case MSG_TYPE_NORMAL:
		caller = s.normalDispatcher
	case MSG_TYPE_REQUEST:
		caller = s.requestDispatcher
	case MSG_TYPE_CALL:
		caller = s.callDispatcher
	default:
		panic("not support msgType")
	}
	return caller
}

//function's first parameter must ServiceID
//isAutoReply: is auto reply when msgType is request or call.
func (s *Skeleton) SubscribeFunc(msgType int32, id interface{}, fun interface{}, isAutoReply bool) {
	caller := s.findCallerByType(msgType)
	switch key := id.(type) {
	case int:
		caller.AddFuncInt(key, fun)
	case string:
		caller.AddFunc(key, fun)
	}
	caller.setIsAutoReply(id, isAutoReply)
}

//method's first parameter must ServiceID
//isAutoReply: is auto reply when msgType is request or call.
func (s *Skeleton) SubscribeMethod(msgType int32, id interface{}, v interface{}, methodName string, isAutoReply bool) {
	caller := s.findCallerByType(msgType)
	switch key := id.(type) {
	case int:
		caller.AddMethodInt(key, v, methodName)
	case string:
		caller.AddMethod(key, v, methodName)
	}
	caller.setIsAutoReply(id, isAutoReply)
}

func (s *Skeleton) OnDistributeMSG(msg *Message) {
}
func (s *Skeleton) OnCloseNotify() {
	s.SendClose(s.s.getId(), true)
}
