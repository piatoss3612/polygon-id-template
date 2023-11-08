import { useState, useEffect, useRef } from "react";
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  Button,
  useDisclosure,
  Spinner,
  Center,
} from "@chakra-ui/react";
import { ExternalLinkIcon } from "@chakra-ui/icons";
import QRCode from "react-qr-code";

const linkDownloadPolygonIDWalletApp =
  "https://0xpolygonid.github.io/tutorials/wallet/wallet-overview/#quick-start";

function PolygonIDVerifier({
  credentialType,
  issuerOrHowToLink,
  onVerificationResult,
  publicServerURL,
  localServerURL,
}) {
  const { isOpen, onOpen, onClose } = useDisclosure();
  const [sessionId, setSessionId] = useState("");
  const [qrCodeData, setQrCodeData] = useState();
  const [isHandlingVerification, setIsHandlingVerification] = useState(false);
  const [verificationCheckComplete, setVerificationCheckComplete] =
    useState(false);
  const [verificationMessage, setVerificationMessage] = useState("");
  const [socketEvents, setSocketEvents] = useState([]);

  const socket = useRef();

  // serverUrl is localServerURL if not running in prod
  // Note: the verification callback will always come from the publicServerURL
  const serverUrl = window.location.href.startsWith("https")
    ? publicServerURL
    : localServerURL;

  const getQrCodeApi = (sessionId) => {
    if (credentialType === "Authorization") {
      return serverUrl + `/api/get-login-qr?sessionId=${sessionId}`;
    }

    return serverUrl + `/api/get-auth-qr?sessionId=${sessionId}`;
  }

  useEffect(() => {
    const _socket = new WebSocket("ws://" + "localhost:8080" + "/ws");
    socket.current = _socket;

    _socket.onopen = (e) => {
      console.log("connected to socket");
      console.log(e)
    }

    _socket.onclose = (e) => {
      console.log("disconnected from socket")
      console.log(e)
    }

    _socket.onmessage = (e) => {
      let msg = JSON.parse(e.data);

      console.log(msg)

      switch (msg.type) {
        case "id":
          setSessionId(msg.id);
          break;
        case "event":
          setSocketEvents((socketEvents) => [...socketEvents, msg.event]);
          break;
        default:
          console.log("unknown message type:", msg.type);
      }
    }

    _socket.onerror = (e) => {
      console.log("error")
      console.log(e)
    }
  }, []);

  useEffect(() => {
    const fetchQrCode = async () => {
      const response = await fetch(getQrCodeApi(sessionId));
      const data = await response.text();
      return JSON.parse(data);
    };

    if (sessionId) {
      fetchQrCode().then(setQrCodeData).catch(console.error);
    }
  }, [sessionId]);

  // socket event side effects
  useEffect(() => {
    if (socketEvents.length) {
      const currentSocketEvent = socketEvents[socketEvents.length - 1];

      console.log(currentSocketEvent)

      if (currentSocketEvent.fn === "handleVerification" || currentSocketEvent.fn === "handleLogin") {
        if (currentSocketEvent.status === "IN_PROGRESS") {
          setIsHandlingVerification(true);
        } else {
          setIsHandlingVerification(false);
          setVerificationCheckComplete(true);
          if (currentSocketEvent.status === "DONE") {
            setVerificationMessage("✅ Verified proof");
            setTimeout(() => {
              reportVerificationResult(true);
            }, "2000");
            socket.current.close();
          } else {
            setVerificationMessage("❌ Error verifying VC");
          }
        }
      }
    }
  }, [socketEvents]);

  // callback, send verification result back to app
  const reportVerificationResult = (result) => {
    onVerificationResult(result);
  };

  function openInNewTab(url) {
    var win = window.open(url, "_blank");
    win.focus();
  }

  return (
    <div>
      {sessionId ? (
        <Button colorScheme="purple" onClick={onOpen} margin={4}>
          Prove access rights
        </Button>
      ) : (
        <Spinner />
      )}

      {qrCodeData && (
        <Modal isOpen={isOpen} onClose={onClose} isCentered>
          <ModalOverlay />
          <ModalContent>
            <ModalHeader>
              Scan this QR code from your{" "}
              <a
                href={linkDownloadPolygonIDWalletApp}
                target="_blank"
                rel="noreferrer"
              >
                Polygon ID Wallet App
              </a>{" "}
              to prove access rights
            </ModalHeader>
            <ModalCloseButton />
            <ModalBody textAlign={"center"} fontSize={"12px"}>
              {isHandlingVerification && (
                <div>
                  <p>Authenticating...</p>
                  <Spinner size={"xl"} colorScheme="purple" my={2} />
                </div>
              )}
              {verificationMessage}
              {qrCodeData &&
                !isHandlingVerification &&
                !verificationCheckComplete && (
                  <Center marginBottom={1}>
                    <QRCode value={JSON.stringify(qrCodeData)} />
                  </Center>
                )}

              {qrCodeData.body?.scope[0]?.query && (
                <p>Type: {qrCodeData.body?.scope[0].query.type}</p>
              )}

              {qrCodeData.body.message && <p>{qrCodeData.body.message}</p>}

              {qrCodeData.body.reason && (
                <p>Reason: {qrCodeData.body.reason}</p>
              )}
            </ModalBody>

            <ModalFooter>
              <Button
                fontSize={"10px"}
                margin={1}
                colorScheme="purple"
                onClick={() => openInNewTab(linkDownloadPolygonIDWalletApp)}
              >
                Download the Polygon ID Wallet App{" "}
                <ExternalLinkIcon marginLeft={2} />
              </Button>
              <Button
                fontSize={"10px"}
                margin={1}
                colorScheme="purple"
                onClick={() => openInNewTab(issuerOrHowToLink)}
              >
                Get a {credentialType} VC <ExternalLinkIcon marginLeft={2} />
              </Button>
            </ModalFooter>
          </ModalContent>
        </Modal>
      )}
    </div>
  );
}

export default PolygonIDVerifier;
