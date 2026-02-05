package exporter

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

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

	bufferData := buff.data()[buffKey]
	if bufferData == nil {
		buff.dataMap[buffKey] = lv
		bufferData = lv
	} else {
		for _, mp := range meterParams {
			existingVal = bufferData.metersData[mp.Name]
			readedVal = lv.metersData[mp.Name]
			if readedVal != nil {
				if existingVal != nil {
					bufferData.metersData[mp.Name] = mp.applyValue(readedVal, existingVal)
				} else {
					bufferData.metersData[mp.Name] = readedVal
				}
			}
		}
		lv.clear()
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

func (buff *labeledValuesCollection) data() labeledValuesMap {
	return buff.dataMap
}

func (buff *labeledValuesCollection) deleteByKey(key labeledValuesMapKey) {
	lv := buff.dataMap[key]
	if lv != nil {
		lv.clear()
	}
	buff.dataMap[key] = nil
	delete(buff.dataMap, key)
}

func (buff *labeledValuesCollection) get(key labeledValuesMapKey) (*labeledValues, bool) {
	val, ok := buff.dataMap[key]
	return val, ok
}

func (buff *labeledValuesCollection) set(key labeledValuesMapKey, lv *labeledValues) {
	if lv != nil {
		buff.dataMap[key] = lv
	} else if _, ok := buff.get(key); ok {
		buff.deleteByKey(key)
	}
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
