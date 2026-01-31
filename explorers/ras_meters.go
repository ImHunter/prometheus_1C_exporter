package exporter

import (
	"strconv"
	"strings"
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
	// При true, новое значение заменит старое, только если новое больше старого.
	// При false новое значение всегда перетирает старое. В основном применяется для растущих счетчиков *total
	ApplyMax bool
	// Тип данных счетчика. По умолчанию MeterDataUndefined.
	DataType meterDataType
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
	return mp
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

	return &mp
}

func (mp *MeterParams) readValue(item *map[string]string) *int64 {

	var txt string
	var retVal int64

	txt = (*item)[mp.SourceField]
	if txt == "" && len(mp.OtherSourceFields) != 0 {
		for _, fn := range mp.OtherSourceFields {
			txt = (*item)[fn]
			if txt != "" {
				break
			}
		}
	}

	if txt == "" {
		return nil
	}

	if mp.DataType == MeterDataDuration || mp.DataType == MeterDataUnixDate {
		st, e := time.ParseInLocation("2006-01-02T15:04:05", txt, localTimeLocation)
		if e == nil {
			if mp.DataType == MeterDataDuration {
				retVal = int64(time.Since(st).Seconds())
			} else {
				retVal = st.Unix()
			}
		} else {
			return nil
		}
	} else {
		return atoi(txt)
	}

	return &retVal
}

func atoi(n string) *int64 {
	if v, err := strconv.ParseInt(n, 10, 64); err == nil {
		return &v
	}
	return nil
}
