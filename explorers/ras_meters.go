package exporter

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
	// Данные счетчиков обновляются с каждым чтением из rac. ApplyMax регулирует, как использовать новое значение счетчика.
	// При true, новое значение заменит старое, если новое больше старого.
	// При false новое значение всегда перетирает старое. В основном, применяется для растущих счетчиков *total
	ApplyMax bool
	// Тип данных счетчика. По умолчанию MeterDataUndefined.
	DataType meterDataType
	// Функция чтения параметра
	reader ValueReader
}

func (mp *MeterParams) setName(paramName string) *MeterParams {
	mp.Name = paramName
	return mp
}

func (mp *MeterParams) setOtherSourceFields(otherSourceFields []string) *MeterParams {
	mp.OtherSourceFields = otherSourceFields
	return mp
}

func (mp *MeterParams) setDataType(dataType meterDataType) *MeterParams {
	mp.DataType = dataType
	mp.resetReader()
	return mp
}

func (mp *MeterParams) getReader() ValueReader {
	if mp.reader == nil {
		// Ищем в реестре
		if reader, exists := readerRegistry[mp.DataType]; exists {
			mp.reader = reader
		} else {
			mp.reader = defaultReader
		}
	}
	return mp.reader
}

// Основной метод чтения значения
func (mp *MeterParams) readValue(item *map[string]string) *int64 {
	return mp.getReader()(item, mp)
}

// Вспомогательный метод для извлечения значения
func (mp *MeterParams) extractValue(item *map[string]string) string {
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

func (mp *MeterParams) resetReader() {
	mp.reader = nil // при следующем вызове getReader он переинициализируется
}

type MeterParamsCollection []*MeterParams

func (allParams *MeterParamsCollection) add(sourceField string, description string, applyMax bool) *MeterParams {

	mp := MeterParams{
		Description: description,
		SourceField: sourceField,
		ApplyMax:    applyMax,
		DataType:    MeterDataUndefined,
	}
	mp.Name = sourceField
	mp.Name = strings.ReplaceAll(mp.Name, "-", "")
	mp.Name = strings.ReplaceAll(mp.Name, " ", "")

	*allParams = append(*allParams, &mp)

	mp.setDataType(mp.DataType)

	return &mp
}

// func (mp *MeterParams) readValue(item *map[string]string) *int64 {

// 	var txt string
// 	var retVal int64

// 	txt = strings.ToLower((*item)[mp.SourceField])
// 	if txt == "" && len(mp.OtherSourceFields) != 0 {
// 		for _, fn := range mp.OtherSourceFields {
// 			txt = (*item)[fn]
// 			if txt != "" {
// 				break
// 			}
// 		}
// 	}

// 	if txt == "" {
// 		return nil
// 	}

// 	if mp.DataType == MeterDataDuration || mp.DataType == MeterDataUnixDate {
// 		st, e := time.ParseInLocation("2006-01-02T15:04:05", txt, localTimeLocation)
// 		if e == nil {
// 			if mp.DataType == MeterDataDuration {
// 				retVal = int64(time.Since(st).Seconds())
// 			} else {
// 				retVal = st.Unix()
// 			}
// 		} else {
// 			return nil
// 		}
// 	} else if mp.DataType == MeterDataOnOff {
// 		if txt == "off" {
// 			retVal = 0
// 		} else if txt == "on" {
// 			retVal = 1
// 		} else {
// 			return nil
// 		}
// 	} else {
// 		return atoi(txt)
// 	}

// 	return &retVal
// }

// Type alias для функции чтения
type ValueReader func(item *map[string]string, mp *MeterParams) *int64

// Глобальный реестр функций чтения (инициализируется один раз)
var (
	readerRegistry     map[meterDataType]ValueReader
	readerRegistryOnce sync.Once
	defaultReader      ValueReader
)

func InitReaders() {
	readerRegistryOnce.Do(func() {
		readerRegistry = make(map[meterDataType]ValueReader)

		// Известные ридеры
		readerRegistry[MeterDataNumber] = createNumberReader()
		readerRegistry[MeterDataUnixDate] = createUnixDateReader()
		readerRegistry[MeterDataDuration] = createDurationReader()
		readerRegistry[MeterDataOnOff] = createOnOffReader()

		// Ридер по умолчанию или для неизвестных типов
		defaultReader = readerRegistry[MeterDataNumber]
	})
}

// Фабричные методы для создания функций (вызываются один раз)

func createNumberReader() ValueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		txt := mp.extractValue(item)
		if txt == "" {
			return nil
		}
		val := atoi(txt)
		return val
	}
}

func createUnixDateReader() ValueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		txt := mp.extractValue(item)
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

func createDurationReader() ValueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		txt := mp.extractValue(item)
		if txt == "" {
			return nil
		}
		t, err := parseTime(txt)
		if err != nil {
			return nil
		}
		// Можно добавить опциональную логику для referenceTime
		retVal := int64(time.Since(t).Seconds())
		return &retVal
	}
}

func createOnOffReader() ValueReader {
	return func(item *map[string]string, mp *MeterParams) *int64 {
		retVal := new(int64)
		txt := strings.ToLower(mp.extractValue(item))
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

// Структурированное хранение данных сессии, прочитанных из rac
type labeledValues struct {
	// Значения идентификаторов сессии ("base", "user" и т.п)
	labelsData map[string]string
	// Значения счетчиков сессии ("memorytotal" и т.п.)
	metersData map[string]*int64
}

func newLabeledValues() *labeledValues {
	sd := labeledValues{
		labelsData: make(map[string]string),
		metersData: make(map[string]*int64),
	}
	return &sd
}

func (data *labeledValues) GetWithAll() prometheus.Labels {
	names := []string{}
	for k := range data.labelsData {
		names = append(names, k)
	}
	return data.GetWith(names...)
}

func (data *labeledValues) GetWith(names ...string) prometheus.Labels {
	getWith := make(prometheus.Labels)
	for _, lb := range names {
		getWith[lb] = data.labelsData[lb]
	}
	return getWith
}

func (lv *labeledValues) readMeterValues(rasRowItem *map[string]string, meterParams MeterParamsCollection) {
	var readedVal *int64
	for _, mp := range meterParams {
		readedVal = mp.readValue(rasRowItem)
		if readedVal != nil {
			lv.metersData[mp.Name] = readedVal
		}
	}
}

func (lv *labeledValues) writeToBuf(buff rasDataCollection, meterParams MeterParamsCollection, keyLabel string) {

	var readedVal *int64
	var existingVal *int64

	keyValue := lv.labelsData[keyLabel]

	bufferData := buff[keyValue]
	if bufferData == nil {
		buff[keyValue] = lv
		bufferData = lv
	} else {
		for _, p := range meterParams {
			existingVal = bufferData.metersData[p.Name]
			readedVal = lv.metersData[p.Name]
			if readedVal == nil || (readedVal != nil && existingVal != nil && *readedVal == *existingVal) {
				continue
			}
			if existingVal == nil || !p.ApplyMax {
				if existingVal == nil {
					bufferData.metersData[p.Name] = new(int64)
				}
				*bufferData.metersData[p.Name] = *readedVal
				continue
			}
			if *readedVal > *existingVal {
				*bufferData.metersData[p.Name] = *readedVal
			}
		}
		clear(lv.labelsData)
		clear(lv.metersData)
		lv = nil
	}
}

type rasDataCollection map[string]*labeledValues

func atoi(n string) *int64 {
	if v, err := strconv.ParseInt(n, 10, 64); err == nil {
		return &v
	}
	return nil
}

func parseTime(txt string) (time.Time, error) {

	t, err := time.ParseInLocation("2006-01-02T15:04:05", txt, localTimeLocation)
	if err == nil {
		return t, nil
	}

	return time.Time{}, err
}
