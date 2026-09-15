package structconvert

import (
	"fmt"
	"reflect"
)

// RegisterNillableValue tells c that nilValue represents "no value" for type
// T: a source field holding exactly nilValue converts to a nil pointer on
// the target side, instead of a boxed copy of nilValue.
func RegisterNillableValue[T any](c *Converter, nilValue T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nillable = append(c.nillable, nillableTypeValue{
		typ:   reflect.TypeOf(nilValue),
		value: any(nilValue),
	})
}

// TypeConverterFunc converts a value of type S into a value of type T.
type TypeConverterFunc[S, T any] func(source S) T

// RegisterTypeConverter registers fn as the converter c uses whenever it
// needs to convert a field of type S into a field of type T. It returns an
// error if S and T are identical, or a converter for that pair is already
// registered.
func RegisterTypeConverter[S, T any](c *Converter, fn TypeConverterFunc[S, T]) error {
	var source S
	var target T
	sourceType := reflect.TypeOf(source)
	targetType := reflect.TypeOf(target)

	if sourceType == targetType {
		return fmt.Errorf("structconvert: cannot register a converter for identical type %s", sourceType)
	}

	key := typePair{source: sourceType, target: targetType}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.converters[key]; exists {
		return fmt.Errorf("structconvert: converter already registered for %s -> %s", sourceType, targetType)
	}
	c.converters[key] = reflect.ValueOf(fn)
	return nil
}

// AfterConvertFunc runs after Convert has copied every field from source
// into target, so it can perform cross-field adjustments the field-by-field
// copy cannot express.
type AfterConvertFunc[S, T any] func(source S, target *TargetModifier) error

// RegisterAfterConvertEvent registers fn to run after every Convert[T] call
// converting an S into a T. It returns an error if a hook for that pair is
// already registered.
//
// T does not appear in fn's signature, so Go cannot infer it from the fn
// argument alone — always call this with both type arguments explicit, e.g.
// RegisterAfterConvertEvent[Order, OrderDTO](c, hook).
func RegisterAfterConvertEvent[S, T any](c *Converter, fn AfterConvertFunc[S, T]) error {
	var source S
	var target T
	key := typePair{source: reflect.TypeOf(source), target: reflect.TypeOf(target)}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.afterHooks[key]; exists {
		return fmt.Errorf("structconvert: after-convert hook already registered for %s -> %s", key.source, key.target)
	}
	c.afterHooks[key] = reflect.ValueOf(fn)
	return nil
}

// TargetModifier lets an AfterConvertFunc read and adjust fields on the
// in-progress target struct by name, without needing the target's concrete
// type.
type TargetModifier struct {
	ref reflect.Value
}

// GetValue returns the current value of fieldName on the target.
func (m *TargetModifier) GetValue(fieldName string) (any, error) {
	fieldRef := m.ref.FieldByName(fieldName)
	if fieldRef == (reflect.Value{}) {
		return nil, fmt.Errorf("structconvert: field %q not found", fieldName)
	}
	return fieldRef.Interface(), nil
}

// SetValue sets fieldName on the target to value. value's type must be
// assignable to the field's type.
func (m *TargetModifier) SetValue(fieldName string, value any) error {
	fieldRef := m.ref.FieldByName(fieldName)
	if fieldRef == (reflect.Value{}) {
		return fmt.Errorf("structconvert: field %q not found", fieldName)
	}
	fieldRef.Set(reflect.ValueOf(value))
	return nil
}
