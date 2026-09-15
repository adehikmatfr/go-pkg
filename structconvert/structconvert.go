// Package structconvert converts one struct type into another, field by
// field, matching fields by name via reflection. It handles identical types,
// pointer/interface unwrapping, nested structs, slices, and a handful of
// common cross-type conversions (string<->uuid.UUID, string<->time.Time) out
// of the box; anything else is handled by a custom converter registered on a
// Converter instance.
//
// Each Converter instance owns its own registries (no package-level mutable
// state), so two consumers using this package never see each other's
// registrations and a converter can be built fresh per test.
//
// Go does not support generic methods, so the generic entry points
// (Convert, ConvertSlice, RegisterTypeConverter, ...) are free functions that
// take a *Converter, rather than methods on it.
package structconvert

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Converter holds the custom conversion rules registered on it. The zero
// value is not usable; construct one with New.
//
// A Converter is safe for concurrent use: registration and conversion both
// take an internal lock.
type Converter struct {
	mu sync.RWMutex

	nillable   []nillableTypeValue
	converters map[typePair]reflect.Value
	afterHooks map[typePair]reflect.Value
}

type typePair struct {
	source reflect.Type
	target reflect.Type
}

type nillableTypeValue struct {
	typ   reflect.Type
	value interface{}
}

// New returns an empty Converter ready for registration and use.
func New() *Converter {
	return &Converter{
		converters: make(map[typePair]reflect.Value),
		afterHooks: make(map[typePair]reflect.Value),
	}
}

func getStructRef(obj interface{}, isTarget bool) (reflect.Value, error) {
	ref := reflect.ValueOf(obj)

	if isTarget {
		if ref.Kind() != reflect.Pointer {
			return ref, errors.New("structconvert: target must be a pointer to a struct")
		}
	}
	if ref.Kind() == reflect.Interface {
		ref = ref.Elem()
	}
	if ref.Kind() == reflect.Pointer {
		ref = reflect.Indirect(ref)
	}
	if ref.Kind() != reflect.Struct {
		return ref, errors.New("structconvert: value must be a struct")
	}
	return ref, nil
}

func getSliceRef(obj interface{}) (reflect.Value, error) {
	ref := reflect.ValueOf(obj)
	if ref.Kind() == reflect.Pointer {
		ref = reflect.Indirect(ref)
	}
	if ref.Kind() == reflect.Interface {
		ref = ref.Elem()
	}
	if ref.Kind() != reflect.Slice {
		return ref, errors.New("structconvert: value is not a slice")
	}
	return ref, nil
}

// convertBuiltinType handles the small set of cross-type conversions this
// package knows about without a registered custom converter.
func convertBuiltinType(sourceType reflect.Type, sourceValue interface{}, targetType reflect.Type) (handled bool, targetValue interface{}, err error) {
	switch {
	case sourceType == reflect.TypeOf(uuid.UUID{}) && targetType.Kind() == reflect.String:
		return true, sourceValue.(uuid.UUID).String(), nil
	case sourceType.Kind() == reflect.String && targetType == reflect.TypeOf(uuid.UUID{}):
		s := sourceValue.(string)
		if s == "" {
			return true, uuid.Nil, nil
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return false, nil, fmt.Errorf("structconvert: parse uuid: %w", err)
		}
		return true, id, nil
	case sourceType == reflect.TypeOf(time.Time{}) && targetType.Kind() == reflect.String:
		return true, sourceValue.(time.Time).Format(time.RFC3339Nano), nil
	case sourceType.Kind() == reflect.String && targetType == reflect.TypeOf(time.Time{}):
		t, err := time.Parse(time.RFC3339Nano, sourceValue.(string))
		if err != nil {
			return false, nil, fmt.Errorf("structconvert: parse time: %w", err)
		}
		return true, t, nil
	case sourceType.ConvertibleTo(targetType):
		return true, reflect.ValueOf(sourceValue).Convert(targetType).Interface(), nil
	default:
		return false, nil, nil
	}
}

func (c *Converter) isNillableValue(sourceFieldType reflect.Type, sourceFieldValue interface{}) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, n := range c.nillable {
		if n.typ == sourceFieldType && n.value == sourceFieldValue {
			return true
		}
	}
	return false
}

func (c *Converter) customConverter(sourceType, targetType reflect.Type) (reflect.Value, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fn, ok := c.converters[typePair{source: sourceType, target: targetType}]
	return fn, ok
}

func (c *Converter) afterHook(sourceType, targetType reflect.Type) (reflect.Value, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fn, ok := c.afterHooks[typePair{source: sourceType, target: targetType}]
	return fn, ok
}

func (c *Converter) targetValue(sourceFieldRef reflect.Value, sourceFieldType reflect.Type, sourceFieldValue interface{}, targetFieldRef reflect.Value, targetFieldType reflect.Type) (interface{}, error) {
	if targetFieldType == sourceFieldType {
		return sourceFieldValue, nil
	}

	if fn, ok := c.customConverter(sourceFieldType, targetFieldType); ok {
		out := fn.Call([]reflect.Value{reflect.ValueOf(sourceFieldValue)})
		return out[0].Interface(), nil
	}

	handled, tValue, err := convertBuiltinType(sourceFieldType, sourceFieldValue, targetFieldType)
	if err != nil {
		return nil, err
	}
	if handled {
		return tValue, nil
	}

	// Target is a pointer, source is a plain value: box it, unless the
	// source value is a registered "nil" sentinel for its type.
	if targetFieldType.Kind() == reflect.Pointer && sourceFieldRef.Kind() != reflect.Pointer && sourceFieldRef.Kind() != reflect.Interface {
		if c.isNillableValue(sourceFieldType, sourceFieldValue) {
			return nil, nil
		}
		newTargetRef := reflect.New(targetFieldType.Elem())
		newTargetValue, err := c.targetValue(sourceFieldRef, sourceFieldType, sourceFieldValue, newTargetRef.Elem(), targetFieldType.Elem())
		if err != nil {
			return nil, err
		}
		newTargetRef.Elem().Set(reflect.ValueOf(newTargetValue))
		return newTargetRef.Interface(), nil
	}

	// Source is a pointer, target is a plain value: unbox it (zero value if nil).
	if sourceFieldRef.Kind() == reflect.Pointer && targetFieldRef.Kind() != reflect.Pointer && targetFieldRef.Kind() != reflect.Interface {
		if sourceFieldRef.IsNil() {
			return reflect.Indirect(reflect.New(targetFieldType)).Interface(), nil
		}
		indirect := reflect.Indirect(sourceFieldRef)
		return c.targetValue(indirect, indirect.Type(), indirect.Interface(), targetFieldRef, targetFieldType)
	}

	// Both sides are structs of different types: recurse.
	if sourceFieldRef.Kind() == reflect.Struct && targetFieldRef.Kind() == reflect.Struct {
		newTargetFieldRef := reflect.Indirect(reflect.New(targetFieldType))
		if err := c.copyFields(sourceFieldRef, newTargetFieldRef); err != nil {
			return nil, err
		}
		return newTargetFieldRef.Interface(), nil
	}

	return nil, fmt.Errorf("structconvert: cannot convert field of type %s to %s", sourceFieldType, targetFieldType)
}

func (c *Converter) copyFields(sourceRef, targetRef reflect.Value) error {
	fieldCount := sourceRef.NumField()
	for i := 0; i < fieldCount; i++ {
		sourceField := sourceRef.Type().Field(i)
		if !sourceField.IsExported() {
			continue
		}

		sourceFieldRef := sourceRef.Field(i)
		sourceFieldType := sourceFieldRef.Type()
		sourceFieldValue := sourceFieldRef.Interface()

		targetFieldRef := targetRef.FieldByName(sourceField.Name)
		if targetFieldRef == (reflect.Value{}) {
			continue
		}
		targetField, found := targetRef.Type().FieldByName(sourceField.Name)
		if !found || !targetField.IsExported() {
			continue
		}
		targetFieldType := targetFieldRef.Type()

		if sourceFieldType != targetFieldType && sourceFieldRef.Kind() == reflect.Slice && targetFieldRef.Kind() == reflect.Slice {
			if err := c.copySlice(sourceFieldRef, targetFieldRef); err != nil {
				return err
			}
			continue
		}

		value, err := c.targetValue(sourceFieldRef, sourceFieldType, sourceFieldValue, targetFieldRef, targetFieldType)
		if err != nil {
			return err
		}
		if value == nil {
			targetFieldRef.Set(reflect.Zero(targetFieldRef.Type()))
		} else {
			targetFieldRef.Set(reflect.ValueOf(value))
		}
	}

	if fn, ok := c.afterHook(sourceRef.Type(), targetRef.Type()); ok {
		out := fn.Call([]reflect.Value{sourceRef, reflect.ValueOf(&TargetModifier{ref: targetRef})})
		if !out[0].IsNil() {
			return out[0].Interface().(error)
		}
	}
	return nil
}

func (c *Converter) copySlice(sourceFieldRef, targetFieldRef reflect.Value) error {
	elemType := targetFieldRef.Type().Elem()
	out := reflect.MakeSlice(reflect.SliceOf(elemType), 0, sourceFieldRef.Len())

	for i := 0; i < sourceFieldRef.Len(); i++ {
		sourceElem := sourceFieldRef.Index(i)
		targetElem := reflect.Indirect(reflect.New(elemType))

		value, err := c.targetValue(sourceElem, sourceElem.Type(), sourceElem.Interface(), targetElem, elemType)
		if err != nil {
			return err
		}
		if value != nil {
			out = reflect.Append(out, reflect.ValueOf(value))
		} else {
			out = reflect.Append(out, reflect.Indirect(reflect.New(elemType)))
		}
	}

	targetFieldRef.Set(out)
	return nil
}
