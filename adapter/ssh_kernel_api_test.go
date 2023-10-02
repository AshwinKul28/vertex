package adapter

import (
	"net/http"
	"testing"

	"github.com/h2non/gock"
	"github.com/stretchr/testify/suite"
	"github.com/vertex-center/vertex/config"
	"github.com/vertex-center/vertex/types"
)

type SshKernelApiAdapterTestSuite struct {
	suite.Suite

	adapter SshKernelApiAdapter
}

func TestSshKernelApiAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(SshKernelApiAdapterTestSuite))
}

func (suite *SshKernelApiAdapterTestSuite) SetupTest() {
	suite.adapter = *NewSshKernelApiAdapter().(*SshKernelApiAdapter)
}

func (suite *SshKernelApiAdapterTestSuite) TestGetAll() {
	gock.Off()
	gock.New(config.Current.HostKernel).
		Get("/api/security/ssh").
		Reply(http.StatusOK).
		JSON([]types.PublicKey{})

	keys, err := suite.adapter.GetAll()
	suite.NoError(err)
	suite.Len(keys, 0)
}

func (suite *SshKernelApiAdapterTestSuite) TestAdd() {
	gock.Off()
	gock.New(config.Current.HostKernel).
		Post("/api/security/ssh").
		Reply(http.StatusOK)

	err := suite.adapter.Add("key")
	suite.NoError(err)
}

func (suite *SshKernelApiAdapterTestSuite) TestDelete() {
	gock.Off()
	gock.New(config.Current.HostKernel).
		Delete("/api/security/ssh/fingerprint").
		Reply(http.StatusOK)

	err := suite.adapter.Remove("fingerprint")
	suite.NoError(err)
}