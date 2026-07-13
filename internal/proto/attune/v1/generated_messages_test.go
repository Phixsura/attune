package attunev1

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type registeredGeneratedMessage interface {
	Reset()
	String() string
	ProtoReflect() protoreflect.Message
	Descriptor() ([]byte, []int)
}

func TestRegisteredGeneratedMessagesCommonMethods(t *testing.T) {
	messages := registeredAttuneGeneratedMessages(t)
	for _, mt := range messages {
		fullName := mt.Descriptor().FullName()
		t.Run(generatedProtoTestName(fullName), func(t *testing.T) {
			msg, ok := mt.New().Interface().(registeredGeneratedMessage)
			if !ok {
				t.Fatalf("%s does not expose generated message methods", fullName)
			}

			value := reflect.ValueOf(msg)
			populateGeneratedMessage(value, 0)
			callRegisteredGeneratedGetters(t, value)
			callRegisteredGeneratedGetters(t, reflect.Zero(value.Type()))
			touchRegisteredGeneratedMessage(t, msg, fullName)
			touchRegisteredNilProtoReflect(t, value.Type(), fullName)
		})
	}
}

func TestSlackGeneratedMessagesExposeProtoMessage(t *testing.T) {
	t.Parallel()
	ptrext.Of(SlackConnConfig{}).ProtoMessage()
	ptrext.Of(SlackChannel{}).ProtoMessage()
	ptrext.Of(DiscoverSlackChannelsRequest{}).ProtoMessage()
	ptrext.Of(DiscoverSlackChannelsResponse{}).ProtoMessage()
}

func registeredAttuneGeneratedMessages(t *testing.T) []protoreflect.MessageType {
	t.Helper()
	var messages []protoreflect.MessageType
	protoregistry.GlobalTypes.RangeMessages(func(mt protoreflect.MessageType) bool {
		desc := mt.Descriptor()
		if desc.IsMapEntry() || !strings.HasPrefix(string(desc.FullName()), "attune.v1.") {
			return true
		}
		messages = append(messages, mt)
		return true
	})
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Descriptor().FullName() < messages[j].Descriptor().FullName()
	})
	if len(messages) < 400 {
		t.Fatalf("registered attune messages = %d, want at least 400", len(messages))
	}
	return messages
}

func touchRegisteredGeneratedMessage(
	t *testing.T,
	msg registeredGeneratedMessage,
	want protoreflect.FullName,
) {
	t.Helper()
	_ = msg.String()
	if got := msg.ProtoReflect().Descriptor().FullName(); got != want {
		t.Fatalf("%T descriptor name = %s, want %s", msg, got, want)
	}
	if raw, path := msg.Descriptor(); len(raw) == 0 || len(path) == 0 {
		t.Fatalf("%T Descriptor() returned empty raw=%d path=%d", msg, len(raw), len(path))
	}
	msg.Reset()
	if got := msg.ProtoReflect().Descriptor().FullName(); got != want {
		t.Fatalf("%T descriptor after Reset = %s, want %s", msg, got, want)
	}
}

func touchRegisteredNilProtoReflect(
	t *testing.T,
	typ reflect.Type,
	want protoreflect.FullName,
) {
	t.Helper()
	nilValue := reflect.Zero(typ)
	got := nilValue.MethodByName("ProtoReflect").Call(nil)
	if len(got) != 1 {
		t.Fatalf("%s nil ProtoReflect returned %d values", typ, len(got))
	}
	msg := got[0].Interface().(protoreflect.Message)
	if gotName := msg.Descriptor().FullName(); gotName != want {
		t.Fatalf("%s nil descriptor name = %s, want %s", typ, gotName, want)
	}
	if raw := nilValue.MethodByName("Descriptor").Call(nil); len(raw) != 2 {
		t.Fatalf("%s nil Descriptor returned %d values", typ, len(raw))
	}
}

func callRegisteredGeneratedGetters(t *testing.T, value reflect.Value) {
	t.Helper()
	typ := value.Type()
	for i := range typ.NumMethod() {
		method := typ.Method(i)
		if strings.HasPrefix(method.Name, "Get") &&
			method.Type.NumIn() == 1 &&
			method.Type.NumOut() == 1 {
			value.Method(i).Call(nil)
		}
	}
}

func populateGeneratedMessage(value reflect.Value, depth int) {
	if !value.IsValid() || depth > 4 {
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		populateGeneratedMessage(value.Elem(), depth+1)
		return
	}
	if value.Kind() != reflect.Struct {
		populateGeneratedField(value, depth+1)
		return
	}
	for i := range value.NumField() {
		field := value.Field(i)
		if field.CanSet() {
			populateGeneratedField(field, depth+1)
		}
	}
}

func populateGeneratedField(field reflect.Value, depth int) {
	if !field.CanSet() || depth > 4 {
		return
	}
	switch field.Kind() {
	case reflect.Bool:
		field.SetBool(true)
	case reflect.String:
		field.SetString("value")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		field.SetUint(1)
	case reflect.Float32, reflect.Float64:
		field.SetFloat(1.25)
	case reflect.Pointer:
		elem := reflect.New(field.Type().Elem())
		if elem.Elem().Kind() == reflect.Struct {
			populateGeneratedMessage(elem, depth+1)
		} else {
			populateGeneratedField(elem.Elem(), depth+1)
		}
		field.Set(elem)
	case reflect.Slice:
		elem := reflect.New(field.Type().Elem()).Elem()
		populateGeneratedField(elem, depth+1)
		field.Set(reflect.Append(field, elem))
	case reflect.Map:
		key := reflect.New(field.Type().Key()).Elem()
		value := reflect.New(field.Type().Elem()).Elem()
		populateGeneratedField(key, depth+1)
		populateGeneratedField(value, depth+1)
		mapping := reflect.MakeMap(field.Type())
		mapping.SetMapIndex(key, value)
		field.Set(mapping)
	case reflect.Struct:
		populateGeneratedMessage(field, depth+1)
	}
}

func generatedProtoTestName(name protoreflect.FullName) string {
	return strings.ReplaceAll(string(name), ".", "_")
}
