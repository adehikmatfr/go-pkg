package structconvert_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/adehikmatfr/go-pkg/v2/structconvert"
)

type Address struct {
	City string
}

type UserModel struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
	Address   Address
	Tags      []string
	Age       int
}

type UserDTO struct {
	ID        string
	Name      string
	CreatedAt string
	Address   AddressDTO
	Tags      []string
	Age       *int
}

type AddressDTO struct {
	City string
}

func TestConvertBasicFields(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()
	source := UserModel{
		ID:        id,
		Name:      "Alice",
		CreatedAt: now,
		Address:   Address{City: "Jakarta"},
		Tags:      []string{"a", "b"},
		Age:       30,
	}

	c := structconvert.New()
	dto, err := structconvert.Convert[UserDTO](c, source)
	if err != nil {
		t.Fatalf("Convert() error: %v", err)
	}

	if dto.ID != id.String() {
		t.Errorf("ID = %q, want %q", dto.ID, id.String())
	}
	if dto.Name != "Alice" {
		t.Errorf("Name = %q, want %q", dto.Name, "Alice")
	}
	if dto.CreatedAt != now.Format(time.RFC3339Nano) {
		t.Errorf("CreatedAt = %q, want %q", dto.CreatedAt, now.Format(time.RFC3339Nano))
	}
	if dto.Address.City != "Jakarta" {
		t.Errorf("Address.City = %q, want %q", dto.Address.City, "Jakarta")
	}
	if len(dto.Tags) != 2 || dto.Tags[0] != "a" {
		t.Errorf("Tags = %v, want [a b]", dto.Tags)
	}
	if dto.Age == nil || *dto.Age != 30 {
		t.Errorf("Age = %v, want pointer to 30", dto.Age)
	}
}

func TestConvertPointerTarget(t *testing.T) {
	c := structconvert.New()
	source := UserModel{Name: "Bob"}
	dto, err := structconvert.Convert[*UserDTO](c, source)
	if err != nil {
		t.Fatalf("Convert() error: %v", err)
	}
	if dto.Name != "Bob" {
		t.Errorf("Name = %q, want %q", dto.Name, "Bob")
	}
}

func TestConvertSlice(t *testing.T) {
	c := structconvert.New()
	sources := []UserModel{{Name: "A"}, {Name: "B"}}
	dtos, err := structconvert.ConvertSlice[UserDTO](c, sources)
	if err != nil {
		t.Fatalf("ConvertSlice() error: %v", err)
	}
	if len(dtos) != 2 || dtos[0].Name != "A" || dtos[1].Name != "B" {
		t.Errorf("ConvertSlice() = %+v, want [A B]", dtos)
	}
}

func TestMustConvertPanicsOnError(t *testing.T) {
	c := structconvert.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustConvert() should panic when Convert would error")
		}
	}()
	structconvert.MustConvert[UserDTO](c, "not a struct")
}

type Source struct{ Cents int64 }
type Target struct{ Cents string }

func TestRegisterTypeConverter(t *testing.T) {
	c := structconvert.New()
	err := structconvert.RegisterTypeConverter(c, func(cents int64) string {
		return "cents:" + string(rune('A'+cents))
	})
	if err != nil {
		t.Fatalf("RegisterTypeConverter() error: %v", err)
	}

	target, err := structconvert.Convert[Target](c, Source{Cents: 0})
	if err != nil {
		t.Fatalf("Convert() error: %v", err)
	}
	if target.Cents != "cents:A" {
		t.Errorf("Cents = %q, want %q", target.Cents, "cents:A")
	}
}

func TestRegisterTypeConverterRejectsDuplicateAndIdentical(t *testing.T) {
	c := structconvert.New()
	fn := func(i int64) string { return "" }
	if err := structconvert.RegisterTypeConverter(c, fn); err != nil {
		t.Fatalf("first RegisterTypeConverter() error: %v", err)
	}
	if err := structconvert.RegisterTypeConverter(c, fn); err == nil {
		t.Error("second RegisterTypeConverter() for the same pair should error")
	}

	identical := func(i int64) int64 { return i }
	if err := structconvert.RegisterTypeConverter(c, identical); err == nil {
		t.Error("RegisterTypeConverter() for identical source/target types should error")
	}
}

type WithOptionalScore struct {
	Score int
}

type WithPointerScore struct {
	Score *int
}

func TestRegisterNillableValue(t *testing.T) {
	c := structconvert.New()
	structconvert.RegisterNillableValue(c, -1) // -1 means "no score"

	present, err := structconvert.Convert[WithPointerScore](c, WithOptionalScore{Score: 5})
	if err != nil {
		t.Fatalf("Convert() error: %v", err)
	}
	if present.Score == nil || *present.Score != 5 {
		t.Errorf("Score = %v, want pointer to 5", present.Score)
	}

	absent, err := structconvert.Convert[WithPointerScore](c, WithOptionalScore{Score: -1})
	if err != nil {
		t.Fatalf("Convert() error: %v", err)
	}
	if absent.Score != nil {
		t.Errorf("Score = %v, want nil for the registered nil sentinel", absent.Score)
	}
}

type Order struct {
	Subtotal int
	Tax      int
}

type OrderDTO struct {
	Subtotal int
	Tax      int
	Total    int
}

func TestRegisterAfterConvertEvent(t *testing.T) {
	c := structconvert.New()
	err := structconvert.RegisterAfterConvertEvent[Order, OrderDTO](c, func(source Order, target *structconvert.TargetModifier) error {
		return target.SetValue("Total", source.Subtotal+source.Tax)
	})
	if err != nil {
		t.Fatalf("RegisterAfterConvertEvent() error: %v", err)
	}

	dto, err := structconvert.Convert[OrderDTO](c, Order{Subtotal: 100, Tax: 10})
	if err != nil {
		t.Fatalf("Convert() error: %v", err)
	}
	if dto.Total != 110 {
		t.Errorf("Total = %d, want 110", dto.Total)
	}
}

func TestRegisterAfterConvertEventRejectsDuplicate(t *testing.T) {
	c := structconvert.New()
	hook := func(source Order, target *structconvert.TargetModifier) error { return nil }
	if err := structconvert.RegisterAfterConvertEvent[Order, OrderDTO](c, hook); err != nil {
		t.Fatalf("first RegisterAfterConvertEvent() error: %v", err)
	}
	if err := structconvert.RegisterAfterConvertEvent[Order, OrderDTO](c, hook); err == nil {
		t.Error("second RegisterAfterConvertEvent() for the same pair should error")
	}
}

func TestTargetModifierGetSetUnknownField(t *testing.T) {
	c := structconvert.New()
	err := structconvert.RegisterAfterConvertEvent[Order, OrderDTO](c, func(source Order, target *structconvert.TargetModifier) error {
		if _, err := target.GetValue("Missing"); err == nil {
			t.Error("GetValue() for an unknown field should error")
		}
		if err := target.SetValue("Missing", 1); err == nil {
			t.Error("SetValue() for an unknown field should error")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterAfterConvertEvent() error: %v", err)
	}

	if _, err := structconvert.Convert[OrderDTO](c, Order{}); err != nil {
		t.Fatalf("Convert() error: %v", err)
	}
}

func TestConvertRejectsNonStructSource(t *testing.T) {
	c := structconvert.New()
	if _, err := structconvert.Convert[UserDTO](c, 42); err == nil {
		t.Error("Convert() with a non-struct source should return an error")
	}
}

func TestConvertSliceRejectsNonSlice(t *testing.T) {
	c := structconvert.New()
	if _, err := structconvert.ConvertSlice[UserDTO](c, "not a slice"); err == nil {
		t.Error("ConvertSlice() with a non-slice source should return an error")
	}
}

func TestMustConvertSlice(t *testing.T) {
	c := structconvert.New()
	dtos := structconvert.MustConvertSlice[UserDTO](c, []UserModel{{Name: "A"}})
	if len(dtos) != 1 || dtos[0].Name != "A" {
		t.Errorf("MustConvertSlice() = %+v, want [A]", dtos)
	}
}
