<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <Header id="blackstart-test" version="2007" revision="B" toolID="hand-written"/>
  <Substation name="RINGBUS" desc="A four-breaker ring">
    <VoltageLevel name="V230" desc="230 kV">
      <Voltage unit="V" multiplier="k">230</Voltage>
      <Bay name="BAY1">
        <ConnectivityNode name="N1" pathName="RINGBUS/V230/BAY1/N1"/>
        <ConnectivityNode name="N2" pathName="RINGBUS/V230/BAY1/N2"/>
        <ConductingEquipment name="CB1" type="CBR" desc="Ring breaker 1">
          <Terminal name="T1" connectivityNode="RINGBUS/V230/BAY1/N1"
                    substationName="RINGBUS" voltageLevelName="V230" bayName="BAY1" cNodeName="N1"/>
          <Terminal name="T2" connectivityNode="RINGBUS/V230/BAY1/N2"
                    substationName="RINGBUS" voltageLevelName="V230" bayName="BAY1" cNodeName="N2"/>
          <LNode iedName="IED1" ldInst="CTRL" lnClass="CSWI" lnInst="1" lnType="CSWI_0"/>
        </ConductingEquipment>
        <ConductingEquipment name="LINE1" type="CAB" desc="Feeder">
          <Terminal name="T1" connectivityNode="RINGBUS/V230/BAY1/N1" cNodeName="N1"/>
        </ConductingEquipment>
        <ConductingEquipment name="CT1" type="CTR" desc="Current transformer">
          <Terminal name="T1" connectivityNode="RINGBUS/V230/BAY1/N2" cNodeName="N2"/>
        </ConductingEquipment>
      </Bay>
      <PowerTransformer name="TR1" type="PTR" desc="230/66 kV transformer">
        <TransformerWinding name="W1" type="PTW">
          <Terminal name="T1" connectivityNode="RINGBUS/V230/BAY1/N2" cNodeName="N2"/>
        </TransformerWinding>
        <TransformerWinding name="W2" type="PTW">
          <Terminal name="T2" connectivityNode="RINGBUS/V66/BAY2/N3" cNodeName="N3"/>
        </TransformerWinding>
      </PowerTransformer>
    </VoltageLevel>
    <VoltageLevel name="V66" desc="66 kV">
      <Voltage unit="V" multiplier="k">66</Voltage>
      <Bay name="BAY2">
        <ConnectivityNode name="N3" pathName="RINGBUS/V66/BAY2/N3"/>
        <ConnectivityNode name="N4" pathName="RINGBUS/V66/BAY2/N4"/>
        <ConductingEquipment name="DS1" type="DIS" desc="Isolator">
          <Terminal name="T1" connectivityNode="RINGBUS/V66/BAY2/N3" cNodeName="N3"/>
          <Terminal name="T2" connectivityNode="RINGBUS/V66/BAY2/N4" cNodeName="N4"/>
        </ConductingEquipment>
      </Bay>
    </VoltageLevel>
  </Substation>
  <Communication>
    <SubNetwork name="STATION_BUS" type="8-MMS">
      <ConnectedAP iedName="IED1" apName="AP1">
        <Address>
          <P type="IP">10.0.0.11</P>
        </Address>
        <GSE ldInst="CTRL" cbName="gcbTrip">
          <Address>
            <P type="MAC-Address">01-0C-CD-01-00-01</P>
            <P type="APPID">0001</P>
            <P type="VLAN-ID">005</P>
          </Address>
          <MinTime unit="s" multiplier="m">4</MinTime>
          <MaxTime unit="s" multiplier="m">1000</MaxTime>
        </GSE>
      </ConnectedAP>
    </SubNetwork>
  </Communication>
  <IED name="IED1" type="BAY_CONTROLLER" manufacturer="Nobody" configVersion="1.0">
    <AccessPoint name="AP1">
      <Server>
        <LDevice inst="CTRL">
          <LN0 lnClass="LLN0" lnType="LLN0_0" inst="">
            <DataSet name="dsTrip" desc="Trip signals">
              <FCDA ldInst="CTRL" lnClass="CSWI" lnInst="1" doName="Pos" daName="stVal" fc="ST"/>
            </DataSet>
            <GSEControl name="gcbTrip" datSet="dsTrip" appID="TRIP1" confRev="1" type="GOOSE"/>
          </LN0>
          <LN prefix="" lnClass="CSWI" inst="1" lnType="CSWI_0" desc="Switch controller">
            <Inputs>
              <ExtRef iedName="IED2" ldInst="PROT" lnClass="PTOC" lnInst="1"
                      doName="Op" daName="general" serviceType="GOOSE"
                      srcCBName="gcbProt" srcLDInst="PROT" srcLNClass="LLN0"/>
            </Inputs>
          </LN>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
</SCL>
