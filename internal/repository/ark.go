package repository

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"team_exe/internal/adapters"
	"team_exe/internal/domain/user"
)

type RatingServerRepo struct {
	baseUrl string
}

func NewResultServerRepo(adapter *adapters.ResultServerAdapter) *RatingServerRepo {
	return &RatingServerRepo{baseUrl: adapter.BaseUrl}
}

type updateRequest struct {
	Rating     float64             `json:"rating"`
	Rd         float64             `json:"rd"`
	Volatility float64             `json:"volatility"`
	Games      []gamesResInRequest `json:"games"`
}

type gamesResInRequest struct {
	OppRating float64 `json:"opp_rating"`
	OppRd     float64 `json:"opp_rd"`

	Result float64 `json:"result"`
}

type updateResponse struct {
	NewRating     float64 `json:"new_rating"`
	NewRd         float64 `json:"new_rd"`
	NewVolatility float64 `json:"new_volatility"`
}

func (s *RatingServerRepo) UpdateInfo(oldRating, oldRd, oldVolatility float64, games []user.GameResultElo) (newRating, newRd, newVol float64, err error) {
	method := "/ratings/update"
	resUrl := s.baseUrl + method
	payloadBuf := new(bytes.Buffer)
	gamesToSend := make([]gamesResInRequest, len(games))
	for i := range games {
		gamesToSend[i].OppRating = games[i].OppRating
		gamesToSend[i].OppRd = games[i].OppRd
		if games[i].DidUserWin {
			gamesToSend[i].Result = 1.0
		} else {
			gamesToSend[i].Result = 0.0
		}
	}
	toSend := updateRequest{
		Rating:     oldRating,
		Rd:         oldRd,
		Volatility: oldVolatility,
		Games:      gamesToSend,
	}

	err = json.NewEncoder(payloadBuf).Encode(toSend)
	if err != nil {
		return 0, 0, 0, err
	}
	req, _ := http.NewRequest("POST", resUrl, payloadBuf)

	client := &http.Client{}
	res, e := client.Do(req)
	if e != nil {
		return 0, 0, 0, err
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	var updateResp updateResponse
	if err = decoder.Decode(&updateResp); err != nil {
		return 0, 0, 0, err
	}
	return updateResp.NewRating, updateResp.NewRd, updateResp.NewVolatility, nil
}
