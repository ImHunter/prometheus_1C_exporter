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
	funcValueApplier valueReader
}

func (mp *MeterParams) setName(paramName string) *MeterParams {
	mp.Name = paramName
	return mp
}

func (mp *MeterParams) setOtherSourceFields(otherSourceFields []string) *MeterParams {
	mp.OtherSourceFields = otherSourceFields
	return mp
}

func (mp *MeterParams) setApplyMethod(applyMethod meterApplyMethod) *MeterParams {
	mp.ApplyMethod = applyMethod
	return mp
}

func (mp *MeterParams) setDataType(dataType meterDataType) *MeterParams {
	mp.DataType = dataType
	mp.funcValueReader = getReader(mp.DataType)
	return mp
}

func getReader(dataType meterDataType) valueReader {
	var r valueReader
	if reader, exists := readersRegistry[dataType]; exists {
		r = reader
	} else {
		r = readersRegistry[MeterDataNumber]
	}
	return r
}

func (mp *MeterParams) readValue(item *map[string]string) *int64 {
	return mp.funcValueReader(item, mp)
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

func (allParams *MeterParamsCollection) add(sourceField string, description string) *MeterParams {

	mp := MeterParams{
		Description: description,
		SourceField: sourceField,
		ApplyMethod: ApplyMethodSet,
		DataType:    MeterDataUndefined,
	}
	mp.Name = sourceField
	mp.Name = strings.ReplaceAll(mp.Name, "-", "")
	mp.Name = strings.ReplaceAll(mp.Name, " ", "")

	*allParams = append(*allParams, &mp)

	mp.setDataType(mp.DataType)

	return &mp
}

// Прототип метода чтения значения параметра
type valueReader func(item *map[string]string, mp *MeterParams) *int64

// Прототип метода применения значения параметра
type valueApplier func(item *map[string]string, mp *MeterParams) *int64

var (
	// Глобальный реестр функций чтения (инициализируется один раз)
	readersRegistry    map[meterDataType]valueReader
	appliersRegistry   map[meterDataType]valueApplier
	readerRegistryOnce sync.Once
	// Локальная временная зона
	localTimeLocation *time.Location
)

// Инициализация ридеров
func InitMeterFunctions() {
	readerRegistryOnce.Do(func() {
		readersRegistry = make(map[meterDataType]valueReader)

		// Известные ридеры
		readersRegistry[MeterDataNumber] = createNumberReader()
		readersRegistry[MeterDataUnixDate] = createUnixDateReader()
		readersRegistry[MeterDataDuration] = createDurationReader()
		readersRegistry[MeterDataOnOff] = createOnOffReader()

		// Локальная временная зона, для прочитывания времени
		localTimeLocation, _ = time.LoadLocation("Local")
	})
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

// Структурированное хранение данных сессии, прочитанных из rac
type labeledValues struct {
	// Значения идентификаторов (меток)
	labelsData map[string]string
	// Значения счетчиков ("memorytotal" и т.п.)
	metersData map[string]*int64
}

// Конструктор для labeledValues
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

func (lv *labeledValues) writeToBuf(buff labeledValuesCollection, meterParams MeterParamsCollection) {

	var readedVal *int64
	var existingVal *int64

	buffKey := buff.createKey(lv)

	bufferData := buff.dataMap[buffKey]
	if bufferData == nil {
		buff.dataMap[buffKey] = lv
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

func (lv *labeledValues) getLabelValues(labelNames ...string) []string {
	retVal := make([]string, 0)
	for _, v := range labelNames {
		retVal = append(retVal, lv.labelsData[v])
	}
	return retVal
}

func (lv *labeledValues) clear() {
	for k := range lv.labelsData {
		delete(lv.labelsData, k)
	}
	for k := range lv.metersData {
		delete(lv.metersData, k)
	}
}

// Ключ для буфера данных, может состоять из нескольких элементов (до 3-х полей)
type labeledValuesMapKey [3]string
type labeledValuesMap map[labeledValuesMapKey]*labeledValues
type labeledValuesCollection struct {
	dataMap   labeledValuesMap
	keyFields []string
}

func newLabeledValuesCollection(keys ...string) labeledValuesCollection {
	vc := labeledValuesCollection{
		dataMap:   labeledValuesMap{},
		keyFields: keys,
	}
	return vc
}

func (buff *labeledValuesCollection) deleteByKey(key labeledValuesMapKey) *labeledValuesCollection {
	lv := buff.dataMap[key]
	lv.clear()
	buff.dataMap[key] = nil
	delete(buff.dataMap, key)
	return buff
}

func (buff *labeledValuesCollection) clear() {
	for k, _ := range buff.dataMap {
		buff.deleteByKey(k)
	}
}

func (buff *labeledValuesCollection) createKey(lv *labeledValues) labeledValuesMapKey {
	var retVal labeledValuesMapKey
	for k, v := range buff.keyFields {
		retVal[k] = lv.labelsData[v]
	}
	return retVal
}

type ExemplarChecker struct {
	keys   map[string]map[string]string
	values map[string]map[string]int64
	data   *labeledValuesCollection
}

func newExemplarChecker(buff *labeledValuesCollection, exemplarKeyFields ...string) ExemplarChecker {
	ch := ExemplarChecker{
		data:   buff,
		keys:   make(map[string]map[string]string),
		values: make(map[string]map[string]int64),
	}
	return ch
}

func (finder *ExemplarChecker) isExemplar(lv labeledValues, paramName string) bool {
	// targetSess := finder.keys[base][param]
	return false
}

func (finder *ExemplarChecker) clear() {
	clear(finder.keys)
	clear(finder.values)
}

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
