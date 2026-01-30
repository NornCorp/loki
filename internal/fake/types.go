package fake

import (
	"fmt"
	"time"

	"github.com/brianvoe/gofakeit/v6"
)

// FakeType represents a type of fake data that can be generated
type FakeType string

const (
	// Existing types
	TypeUUID     FakeType = "uuid"
	TypeName     FakeType = "name"
	TypeEmail    FakeType = "email"
	TypeInt      FakeType = "int"
	TypeDecimal  FakeType = "decimal"
	TypeBool     FakeType = "bool"
	TypeDate     FakeType = "date"
	TypeDateTime FakeType = "datetime"
	TypeEnum     FakeType = "enum"
	TypeRef      FakeType = "ref"

	// Person
	TypeFirstName FakeType = "firstname"
	TypeLastName  FakeType = "lastname"
	TypeGender    FakeType = "gender"
	TypeSSN       FakeType = "ssn"

	// Address
	TypeAddress   FakeType = "address"
	TypeStreet    FakeType = "street"
	TypeCity      FakeType = "city"
	TypeState     FakeType = "state"
	TypeZip       FakeType = "zip"
	TypeCountry   FakeType = "country"
	TypeLatitude  FakeType = "latitude"
	TypeLongitude FakeType = "longitude"

	// Contact
	TypePhone          FakeType = "phone"
	TypePhoneFormatted FakeType = "phone_formatted"

	// Company
	TypeCompany       FakeType = "company"
	TypeJobTitle      FakeType = "job_title"
	TypeJobDescriptor FakeType = "job_descriptor"
	TypeJobLevel      FakeType = "job_level"

	// Internet
	TypeURL        FakeType = "url"
	TypeDomain     FakeType = "domain"
	TypeIPv4       FakeType = "ipv4"
	TypeIPv6       FakeType = "ipv6"
	TypeUsername   FakeType = "username"
	TypePassword   FakeType = "password"
	TypeUserAgent  FakeType = "user_agent"
	TypeMacAddress FakeType = "mac_address"

	// Payment
	TypeCreditCard       FakeType = "credit_card"
	TypeCreditCardNumber FakeType = "credit_card_number"
	TypeCreditCardCVV    FakeType = "credit_card_cvv"
	TypeCreditCardExp    FakeType = "credit_card_exp"
	TypeCreditCardType   FakeType = "credit_card_type"

	// Words/Text
	TypeWord      FakeType = "word"
	TypeSentence  FakeType = "sentence"
	TypeParagraph FakeType = "paragraph"
	TypeQuestion  FakeType = "question"
	TypeQuote     FakeType = "quote"

	// Product
	TypeProductName     FakeType = "product_name"
	TypeProductCategory FakeType = "product_category"

	// Color
	TypeColor    FakeType = "color"
	TypeHexColor FakeType = "hex_color"
	TypeRGBColor FakeType = "rgb_color"

	// Misc
	TypeCurrency     FakeType = "currency"
	TypeLanguage     FakeType = "language"
	TypeTimezone     FakeType = "timezone"
	TypeFileExtension FakeType = "file_extension"
	TypeMimeType     FakeType = "mime_type"
)

// FieldConfig defines how to generate fake data for a field
type FieldConfig struct {
	Name   string
	Type   FakeType
	Config map[string]any // Type-specific configuration
}

// RangeConfig defines min/max range for numeric types
type RangeConfig struct {
	Min float64
	Max float64
}

// RefConfig defines reference to another resource
type RefConfig struct {
	Resource string   // Name of the resource to reference
	IDs      []string // Available IDs to choose from
}

// generateUUID generates a random UUID
func generateUUID(faker *gofakeit.Faker, config map[string]any) (any, error) {
	return faker.UUID(), nil
}

// generateName generates a random full name
func generateName(faker *gofakeit.Faker, config map[string]any) (any, error) {
	return faker.Name(), nil
}

// generateEmail generates a random email address
func generateEmail(faker *gofakeit.Faker, config map[string]any) (any, error) {
	return faker.Email(), nil
}

// generateInt generates a random integer
func generateInt(faker *gofakeit.Faker, config map[string]any) (any, error) {
	// Check for range configuration
	if config != nil {
		if minVal, ok := config["min"]; ok {
			if maxVal, ok := config["max"]; ok {
				min := int(minVal.(float64))
				max := int(maxVal.(float64))
				return faker.IntRange(min, max), nil
			}
		}
	}

	// Default range
	return faker.IntRange(0, 1000), nil
}

// generateDecimal generates a random decimal number
func generateDecimal(faker *gofakeit.Faker, config map[string]any) (any, error) {
	// Check for range configuration
	if config != nil {
		if minVal, ok := config["min"]; ok {
			if maxVal, ok := config["max"]; ok {
				min := minVal.(float64)
				max := maxVal.(float64)
				return faker.Float64Range(min, max), nil
			}
		}
	}

	// Default range
	return faker.Float64Range(0, 1000), nil
}

// generateBool generates a random boolean
func generateBool(faker *gofakeit.Faker, config map[string]any) (any, error) {
	return faker.Bool(), nil
}

// generateDate generates a random date
func generateDate(faker *gofakeit.Faker, config map[string]any) (any, error) {
	date := faker.Date()
	return date.Format("2006-01-02"), nil
}

// generateDateTime generates a random datetime
func generateDateTime(faker *gofakeit.Faker, config map[string]any) (any, error) {
	return faker.Date().Format(time.RFC3339), nil
}

// generateEnum selects a random value from provided options
func generateEnum(faker *gofakeit.Faker, config map[string]any) (any, error) {
	if config == nil {
		return nil, fmt.Errorf("enum type requires 'values' configuration")
	}

	values, ok := config["values"]
	if !ok {
		return nil, fmt.Errorf("enum type requires 'values' configuration")
	}

	valuesSlice, ok := values.([]any)
	if !ok {
		return nil, fmt.Errorf("enum values must be an array")
	}

	if len(valuesSlice) == 0 {
		return nil, fmt.Errorf("enum values cannot be empty")
	}

	idx := faker.IntRange(0, len(valuesSlice)-1)
	return valuesSlice[idx], nil
}

// generateRef generates a reference to another resource
func generateRef(faker *gofakeit.Faker, config map[string]any) (any, error) {
	if config == nil {
		return nil, fmt.Errorf("ref type requires 'ids' configuration")
	}

	ids, ok := config["ids"]
	if !ok {
		return nil, fmt.Errorf("ref type requires 'ids' configuration")
	}

	idsSlice, ok := ids.([]string)
	if !ok {
		return nil, fmt.Errorf("ref ids must be a string array")
	}

	if len(idsSlice) == 0 {
		return nil, fmt.Errorf("ref ids cannot be empty")
	}

	idx := faker.IntRange(0, len(idsSlice)-1)
	return idsSlice[idx], nil
}

// typeHandlers maps fake types to their generator functions
var typeHandlers = map[FakeType]func(*gofakeit.Faker, map[string]any) (any, error){
	// Existing
	TypeUUID:     generateUUID,
	TypeName:     generateName,
	TypeEmail:    generateEmail,
	TypeInt:      generateInt,
	TypeDecimal:  generateDecimal,
	TypeBool:     generateBool,
	TypeDate:     generateDate,
	TypeDateTime: generateDateTime,
	TypeEnum:     generateEnum,
	TypeRef:      generateRef,

	// Person
	TypeFirstName: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.FirstName(), nil },
	TypeLastName:  func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.LastName(), nil },
	TypeGender:    func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Gender(), nil },
	TypeSSN:       func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.SSN(), nil },

	// Address
	TypeAddress:   func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Address().Address, nil },
	TypeStreet:    func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Street(), nil },
	TypeCity:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.City(), nil },
	TypeState:     func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.State(), nil },
	TypeZip:       func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Zip(), nil },
	TypeCountry:   func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Country(), nil },
	TypeLatitude:  func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Latitude(), nil },
	TypeLongitude: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Longitude(), nil },

	// Contact
	TypePhone:          func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Phone(), nil },
	TypePhoneFormatted: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.PhoneFormatted(), nil },

	// Company
	TypeCompany:       func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Company(), nil },
	TypeJobTitle:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.JobTitle(), nil },
	TypeJobDescriptor: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.JobDescriptor(), nil },
	TypeJobLevel:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.JobLevel(), nil },

	// Internet
	TypeURL:        func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.URL(), nil },
	TypeDomain:     func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.DomainName(), nil },
	TypeIPv4:       func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.IPv4Address(), nil },
	TypeIPv6:       func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.IPv6Address(), nil },
	TypeUsername:   func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Username(), nil },
	TypePassword:   func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Password(true, true, true, true, false, 12), nil },
	TypeUserAgent:  func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.UserAgent(), nil },
	TypeMacAddress: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.MacAddress(), nil },

	// Payment
	TypeCreditCard:       func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.CreditCard().Number, nil },
	TypeCreditCardNumber: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.CreditCardNumber(nil), nil },
	TypeCreditCardCVV:    func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.CreditCardCvv(), nil },
	TypeCreditCardExp:    func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.CreditCardExp(), nil },
	TypeCreditCardType:   func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.CreditCardType(), nil },

	// Words/Text
	TypeWord:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Word(), nil },
	TypeSentence:  func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Sentence(10), nil },
	TypeParagraph: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Paragraph(3, 5, 10, " "), nil },
	TypeQuestion:  func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Question(), nil },
	TypeQuote:     func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Quote(), nil },

	// Product
	TypeProductName:     func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.ProductName(), nil },
	TypeProductCategory: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.ProductCategory(), nil },

	// Color
	TypeColor:    func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Color(), nil },
	TypeHexColor: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.HexColor(), nil },
	TypeRGBColor: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return fmt.Sprintf("rgb(%d, %d, %d)", f.Number(0, 255), f.Number(0, 255), f.Number(0, 255)), nil },

	// Misc
	TypeCurrency:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.CurrencyShort(), nil },
	TypeLanguage:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.Language(), nil },
	TypeTimezone:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.TimeZone(), nil },
	TypeFileExtension: func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.FileExtension(), nil },
	TypeMimeType:      func(f *gofakeit.Faker, _ map[string]any) (any, error) { return f.FileMimeType(), nil },
}
