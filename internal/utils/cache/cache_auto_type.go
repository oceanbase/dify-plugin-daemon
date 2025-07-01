package cache

import (
	"reflect"
	"time"
)

// Set the value with key
func AutoSet[T any](key string, value T, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	fullTypeInfo := reflect.TypeOf(value)
	pkgPath := fullTypeInfo.PkgPath()
	typeName := fullTypeInfo.Name()
	fullTypeName := pkgPath + "." + typeName

	key = serialKey("auto_type", fullTypeName, key)
	return store(key, value, time.Minute*30, context...)
}

// Get the value with key
func AutoGet[T any](key string, context ...Context) (*T, error) {
	return AutoGetWithGetter(key, func() (*T, error) {
		return nil, ErrNotFound
	}, context...)
}

// Get the value with key, fallback to getter if not found, and set the value to cache
func AutoGetWithGetter[T any](key string, getter func() (*T, error), context ...Context) (*T, error) {
	if client == nil {
		return nil, ErrNotInit
	}

	var result_tmpl T

	// fetch full type info
	fullTypeInfo := reflect.TypeOf(result_tmpl)
	pkgPath := fullTypeInfo.PkgPath()
	typeName := fullTypeInfo.Name()
	fullTypeName := pkgPath + "." + typeName

	key = serialKey("auto_type", fullTypeName, key)
	result, err := get[T](key, context...)
	if err != nil {
		if err == ErrNotFound {
			result, err = getter()
			if err != nil {
				return nil, err
			}

			if err := store(key, result, time.Minute*30, context...); err != nil {
				return nil, err
			}
			return result, nil
		}
		return nil, err
	}

	return result, err
}

// Delete the value with key
func AutoDelete[T any](key string, context ...Context) (int64, error) {
	if client == nil {
		return 0, ErrNotInit
	}

	var result_tmpl T

	fullTypeInfo := reflect.TypeOf(result_tmpl)
	pkgPath := fullTypeInfo.PkgPath()
	typeName := fullTypeInfo.Name()
	fullTypeName := pkgPath + "." + typeName

	key = serialKey("auto_type", fullTypeName, key)
	return del(key, context...)
}
