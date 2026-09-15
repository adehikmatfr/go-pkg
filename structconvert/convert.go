package structconvert

import "reflect"

// Convert copies source's exported fields into a new value of type T,
// applying c's registered custom converters and after-convert hooks. source
// must be a struct or a pointer to one.
func Convert[T any](c *Converter, source any) (T, error) {
	var target T

	sourceRef, err := getStructRef(source, false)
	if err != nil {
		return target, err
	}

	targetType := reflect.TypeOf(target)
	if fn, ok := c.customConverter(sourceRef.Type(), targetType); ok {
		out := fn.Call([]reflect.Value{reflect.ValueOf(source)})
		return out[0].Interface().(T), nil
	}

	if targetType != nil && targetType.Kind() == reflect.Pointer {
		newTarget := reflect.New(targetType.Elem())
		targetRef, err := getStructRef(newTarget.Interface(), true)
		if err != nil {
			return target, err
		}
		if err := c.copyFields(sourceRef, targetRef); err != nil {
			return target, err
		}
		return newTarget.Interface().(T), nil
	}

	targetRef, err := getStructRef(&target, true)
	if err != nil {
		return target, err
	}
	if err := c.copyFields(sourceRef, targetRef); err != nil {
		return target, err
	}
	return target, nil
}

// MustConvert is Convert, panicking on error. Use only where a conversion
// failure is a programmer error (e.g. converting between two types under the
// caller's own control).
func MustConvert[T any](c *Converter, source any) T {
	target, err := Convert[T](c, source)
	if err != nil {
		panic(err)
	}
	return target
}

// ConvertSlice applies Convert to every element of sources, which must be a
// slice or a pointer to one.
func ConvertSlice[T any](c *Converter, sources any) ([]T, error) {
	sourcesRef, err := getSliceRef(sources)
	if err != nil {
		return nil, err
	}

	targets := make([]T, 0, sourcesRef.Len())
	for i := 0; i < sourcesRef.Len(); i++ {
		target, err := Convert[T](c, sourcesRef.Index(i).Interface())
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// MustConvertSlice is ConvertSlice, panicking on error.
func MustConvertSlice[T any](c *Converter, sources any) []T {
	targets, err := ConvertSlice[T](c, sources)
	if err != nil {
		panic(err)
	}
	return targets
}
