package exporter

import (
	"strings"
	"sync"
	"time"
)

// Типы данных счетчика
type meterDataType string

const (
	// Будет работать аналогично MeterDataNumber. Авто-определение не предусмотрено.
	MeterDataUndefined meterDataType = ""
	// Числовой счетчик
	MeterDataNumber meterDataType = "Number"
	// Счетчик по дате. Будет выдавать дату в Unix-дате
	MeterDataUnixDate meterDataType = "UnixDate"
	// Счетчик по дате. Разница между датой-временем наблюдения и датой-временем из данных поля, в секундах.
	MeterDataDuration meterDataType = "Duration"
	// Счетчик полей со значениями On/Off
	MeterDataOnOff meterDataType = "OnOff"
)

type meterApplyMethod string

const (
	// Будет работать аналогично ApplyMethodSet.
	ApplyMethodUndefined meterApplyMethod = ""
	// Устанавливает значение безусловно
	ApplyMethodSet meterApplyMethod = "Set"
	// Устанавливает значение, если оно больше текущего
	ApplyMethodMax meterApplyMethod = "Max"
	// Добавляет значение к текущему
	ApplyMethodAppend meterApplyMethod = "Append"
	// Независимо от значения счетчика, увеличивает значение на 1
	ApplyMethodInc meterApplyMethod = "Inc"
)

// Описание счетчика
type MeterParams struct {
	// Наименование счетчика. Используется в метках и/или именах гистограмм.
	Name string
	// Описание счетчика. Используется в описании гистограммы.
	Description string
	// Имя поля, по которому значение счетчика вычитывается из данных сессии
	SourceField string
	// Опциональные дополнительные поля. Для единичных случаев (и платформ), когда наименование счетчика в данных rac может быть другим.
	// См. https://bugboard.v8.1c.ru/error/000150161
	OtherSourceFields []string
	// Как применять значение счетчика.
	// Данные счетчиков обновляются с каждым чтением из rac. ApplyMethod определяет, как использовать новое значение счетчика.
	ApplyMethod meterApplyMethod
	// Тип данных счетчика. По умолчанию MeterDataUndefined.
	DataType meterDataType
	// Функция чтения значения параметра
	funcValueReader valueReader
	// Функция чтения значения параметра
	funcValueApplier valueApplier
}

func (mp *MeterParams) setName(paramName string) *MeterParams {
	mp.Name = paramName
	return mp
}

func (mp *MeterParams) setSourceField(sourceField string) *MeterParams {
	mp.SourceField = sourceField
	return mp
}

func (mp *MeterParams) setOtherSourceFields(otherSourceFields []string) *MeterParams {
	mp.OtherSourceFields = otherSourceFields
	return mp
}

func (mp *MeterParams) setDataType(dataType meterDataType) *MeterParams {
	mp.DataType = dataType
	mp.funcValueReader = getReader(mp.DataType)
	return mp
}

func (mp *MeterParams) setApplyMethod(applyMethod meterApplyMethod) *MeterParams {
	mp.ApplyMethod = applyMethod
	mp.funcValueApplier = getApplier(applyMethod)
	return mp
}

func (mp *MeterParams) readValue(item *map[string]string) *int64 {
	return mp.funcValueReader(item, mp)
}

func (mp *MeterParams) applyValue(readedVal *int64, existingVal *int64) (*int64, bool) {
	return mp.funcValueApplier(readedVal, existingVal)
}

func (mp *MeterParams) extractTextValue(item *map[string]string) string {
	// Пробуем основное поле
	if val := (*item)[mp.SourceField]; val != "" {
		return val
	}

	// Пробуем альтернативные поля
	for _, field := range mp.OtherSourceFields {
		if val := (*item)[field]; val != "" {
			return val
		}
	}

	return ""
}

type MeterParamsCollection []*MeterParams

func (allParams *MeterParamsCollection) add(sourceField string, description string, applyMethod meterApplyMethod) *MeterParams {

	mp := MeterParams{
		Description: description,
		SourceField: sourceField,
		ApplyMethod: applyMethod,
		DataType:    MeterDataUndefined,
	}
	mp.Name = sourceField
	mp.Name = strings.ReplaceAll(mp.Name, "-", "")
	mp.Name = strings.ReplaceAll(mp.Name, " ", "")

	*allParams = append(*allParams, &mp)

	mp.setDataType(mp.DataType)
	mp.setApplyMethod(applyMethod)

	return &mp
}

// Прототип метода чтения значения параметра
type valueReader func(item *map[string]string, mp *MeterParams) *int64

var (
	// Глобальный реестр функций чтения (инициализируется один раз)
	readersRegistry   map[meterDataType]valueReader
	appliersRegistry  map[meterApplyMethod]valueApplier
	defaultReader     valueReader
	defaultApplier    valueApplier
	funcsRegistryOnce sync.Once
	// Локальная временная зона
	localTimeLocation *time.Location
)

// Инициализация ридеров
func InitMeterFunctions() {
	funcsRegistryOnce.Do(func() {

		// Реестр ридеров
		readersRegistry = make(map[meterDataType]valueReader)
		// Ридеры
		readersRegistry[MeterDataNumber] = createNumberReader()
		readersRegistry[MeterDataUnixDate] = createUnixDateReader()
		readersRegistry[MeterDataDuration] = createDurationReader()
		readersRegistry[MeterDataOnOff] = createOnOffReader()
		defaultReader = readersRegistry[MeterDataNumber]

		// Реестр установщиков значения
		appliersRegistry = make(map[meterApplyMethod]valueApplier)
		// Установщики значения
		appliersRegistry[ApplyMethodSet] = createSetApplier()
		appliersRegistry[ApplyMethodMax] = createMaxApplier()
		appliersRegistry[ApplyMethodAppend] = createAppendApplier()
		appliersRegistry[ApplyMethodInc] = createIncApplier()
		defaultApplier = appliersRegistry[ApplyMethodSet]

		// Локальная временная зона, для прочитывания времени
		localTimeLocation, _ = time.LoadLocation("Local")
	})
}

func getReader(dataType meterDataType) valueReader {
	if dataType == MeterDataUndefined {
		return defaultReader
	} else if reader, exists := readersRegistry[dataType]; exists {
		return reader
	} else {
		return defaultReader
	}
}

func createNumberReader() valueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		txt := mp.extractTextValue(item)
		if txt == "" {
			return nil
		}
		val := atoi(txt)
		return val
	}
}

func createUnixDateReader() valueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		txt := mp.extractTextValue(item)
		if txt == "" {
			return nil
		}
		t, err := parseTime(txt)
		if err != nil {
			return nil
		}
		retVal := t.Unix()
		return &retVal
	}
}

func createDurationReader() valueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		txt := mp.extractTextValue(item)
		if txt == "" {
			return nil
		}
		t, err := parseTime(txt)
		if err != nil {
			return nil
		}
		retVal := int64(time.Since(t).Seconds())
		return &retVal
	}
}

func createOnOffReader() valueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		retVal := new(int64)
		txt := strings.ToLower(mp.extractTextValue(item))
		switch txt {
		case "on":
			*retVal = 1
		case "off":
			*retVal = 0
		default:
			return nil
		}
		return retVal
	}
}

// Прототип метода применения значения параметра
type valueApplier func(readedVal *int64, existingVal *int64) (*int64, bool)

func getApplier(applyMethod meterApplyMethod) valueApplier {
	if applyMethod == ApplyMethodUndefined {
		return defaultApplier
	} else if applier, exists := appliersRegistry[applyMethod]; exists {
		return applier
	} else {
		return defaultApplier
	}
}

func createSetApplier() valueApplier {
	return func(readedVal *int64, existingVal *int64) (*int64, bool) {
		v, i := incompleteValue(readedVal, existingVal)
		if i {
			return v, true
		}
		return readedVal, true
	}
}

func createMaxApplier() valueApplier {
	return func(readedVal *int64, existingVal *int64) (*int64, bool) {
		v, i := incompleteValue(readedVal, existingVal)
		if i {
			return v, true
		}
		if *readedVal > *existingVal {
			return readedVal, true
		} else {
			return existingVal, false
		}
	}
}

func createAppendApplier() valueApplier {
	return func(readedVal *int64, existingVal *int64) (*int64, bool) {
		v, i := incompleteValue(readedVal, existingVal)
		if i {
			return v, true
		}
		retVal := new(int64)
		*retVal = *readedVal + *existingVal
		return retVal, true
	}
}

func createIncApplier() valueApplier {
	return func(readedVal *int64, existingVal *int64) (*int64, bool) {
		retVal := new(int64)
		if existingVal != nil {
			*retVal = *existingVal
		}
		*retVal++
		return retVal, true
	}
}

func incompleteValue(readedVal *int64, existingVal *int64) (*int64, bool) {
	if readedVal != nil && existingVal != nil {
		return readedVal, false
	} else if existingVal != nil {
		return existingVal, true
	} else if readedVal != nil {
		return readedVal, true
	} else {
		return nil, true
	}
}
