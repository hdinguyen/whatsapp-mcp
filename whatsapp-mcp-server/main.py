from typing import List, Dict, Any, Optional
import requests
import os
import json
from datetime import datetime
from mcp.server.fastmcp import FastMCP

# API configuration
WHATSAPP_API_BASE_URL = os.environ.get("BRIDGE_API_URL", "http://localhost:8080/api")
headers = {"x-api-key": os.environ.get("WHATSAPP_API_KEY", "ReplaceWithYourAPIKey")}

# Initialize FastMCP server
mcp = FastMCP("whatsapp")

def make_api_request(endpoint: str, method: str = "POST", payload: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    """Helper method to make API requests to the WhatsApp API server."""
    url = f"{WHATSAPP_API_BASE_URL}/{endpoint}"
    
    try:
        if method.upper() == "GET":
            response = requests.get(url, headers=headers, params=payload)
        else:
            response = requests.post(url, json=payload, headers=headers)
        
        response.raise_for_status()
        return response.text
    except requests.RequestException as e:
        print(f"API request error: {str(e)}")
        return {"success": False, "error": str(e)}
    except json.JSONDecodeError as e:
        print(f"Error parsing response from server: {str(e)}")
        return {"success": False, "error": "Invalid JSON response"}

@mcp.tool()
def search_contacts(query: str) -> List[Dict[str, Any]]:
    """Search WhatsApp contacts by name or phone number.
    
    Args:
        query: Search term to match against contact names or phone numbers
    """
    response = make_api_request("contacts/search", "GET", {"query": query})
    
    return response

@mcp.tool()
def list_messages(
    after: Optional[str] = None,
    before: Optional[str] = None,
    sender_phone_number: Optional[str] = None,
    chat_jid: Optional[str] = None,
    query: Optional[str] = None,
    limit: int = 20,
    page: int = 0,
    include_context: bool = True,
    context_before: int = 1,
    context_after: int = 1
) -> List[Dict[str, Any]]:
    """Get WhatsApp messages matching specified criteria with optional context.
    
    Args:
        after: Optional ISO-8601 formatted string to only return messages after this date
        before: Optional ISO-8601 formatted string to only return messages before this date
        sender_phone_number: Optional phone number to filter messages by sender
        chat_jid: Optional chat JID to filter messages by chat
        query: Optional search term to filter messages by content
        limit: Maximum number of messages to return (default 20)
        page: Page number for pagination (default 0)
        include_context: Whether to include messages before and after matches (default True)
        context_before: Number of messages to include before each match (default 1)
        context_after: Number of messages to include after each match (default 1)
    """
    payload = {
        "limit": limit,
        "page": page,
        "include_context": include_context,
        "context_before": context_before,
        "context_after": context_after
    }
    
    if after:
        payload["after"] = after
    
    if before:
        payload["before"] = before
    
    if sender_phone_number:
        payload["sender_phone_number"] = sender_phone_number
    
    if chat_jid:
        payload["chat_jid"] = chat_jid
    
    if query:
        payload["query"] = query
    
    response = make_api_request("messages", "GET", payload)
    
    return response

@mcp.tool()
def list_chats(
    query: Optional[str] = None,
    limit: int = 20,
    page: int = 0,
    include_last_message: bool = True,
    sort_by: str = "last_active"
) -> List[Dict[str, Any]]:
    """Get WhatsApp chats matching specified criteria.
    
    Args:
        query: Optional search term to filter chats by name or JID
        limit: Maximum number of chats to return (default 20)
        page: Page number for pagination (default 0)
        include_last_message: Whether to include the last message in each chat (default True)
        sort_by: Field to sort results by, either "last_active" or "name" (default "last_active")
    """
    payload = {
        "query": query,
        "limit": limit,
        "page": page,
        "include_last_message": include_last_message,
        "sort_by": sort_by
    }
    
    return make_api_request("chats", "GET", payload)
    
@mcp.tool()
def get_chat(chat_jid: str, include_last_message: bool = True) -> Dict[str, Any]:
    """Get WhatsApp chat metadata by JID.
    
    Args:
        chat_jid: The JID of the chat to retrieve
        include_last_message: Whether to include the last message (default True)
    """
    payload = {
        "chat_jid": chat_jid,
        "include_last_message": include_last_message
    }
    
    return make_api_request("chat", "GET", payload)

@mcp.tool()
def get_direct_chat_by_contact(sender_phone_number: str) -> Dict[str, Any]:
    """Get WhatsApp chat metadata by sender phone number.
    
    Args:
        sender_phone_number: The phone number to search for
    """
    payload = {"phone_number": sender_phone_number}
    
    return make_api_request("chats/by-contact", "GET", payload)

@mcp.tool()
def get_contact_chats(jid: str, limit: int = 20, page: int = 0) -> List[Dict[str, Any]]:
    """Get all WhatsApp chats involving the contact.
    
    Args:
        jid: The contact's JID to search for
        limit: Maximum number of chats to return (default 20)
        page: Page number for pagination (default 0)
    """
    payload = {
        "jid": jid,
        "limit": limit,
        "page": page
    }
    
    return make_api_request("contacts/chats", "GET", payload)
    

@mcp.tool()
def get_last_interaction(jid: str) -> Dict[str, Any]:
    """Get most recent WhatsApp message involving the contact.
    
    Args:
        jid: The JID of the contact to search for
    """
    payload = {"jid": jid}
    
    return make_api_request("contacts/last-interaction", "GET", payload)

@mcp.tool()
def get_message_context(
    message_id: str,
    before: int = 5,
    after: int = 5
) -> Dict[str, Any]:
    """Get context around a specific WhatsApp message.
    
    Args:
        message_id: The ID of the message to get context for
        before: Number of messages to include before the target message (default 5)
        after: Number of messages to include after the target message (default 5)
    """
    payload = {
        "message_id": message_id,
        "before": before,
        "after": after
    }
    
    return make_api_request("message/context", "GET", payload)
    
@mcp.tool()
def send_message(
    recipient: str,
    message: str
) -> Dict[str, Any]:
    """Send a WhatsApp message to a person or group. For group chats use the JID.

    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        message: The message text to send
    
    Returns:
        A dictionary containing success status and a status message
    """
    # Validate input
    if not recipient:
        return {
            "success": False,
            "message": "Recipient must be provided"
        }
    
    payload = {
        "recipient": recipient,
        "message": message
    }
    
    return make_api_request("send", "POST", payload)

@mcp.tool()
def send_file(recipient: str, media_path: str) -> Dict[str, Any]:
    """Send a file such as a picture, raw audio, video or document via WhatsApp to the specified recipient. For group messages use the JID.
    
    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        media_path: The absolute path to the media file to send (image, video, document)
    
    Returns:
        A dictionary containing success status and a status message
    """
    # Validate input
    if not recipient:
        return {
            "success": False,
            "message": "Recipient must be provided"
        }
    
    if not media_path:
        return {
            "success": False,
            "message": "Media path must be provided"
        }
    
    if not os.path.isfile(media_path):
        return {
            "success": False,
            "message": f"Media file not found: {media_path}"
        }
    
    payload = {
        "recipient": recipient,
        "media_path": media_path
    }
    
    return make_api_request("send", "POST", payload)

@mcp.tool()
def send_audio_message(recipient: str, media_path: str) -> Dict[str, Any]:
    """Send any audio file as a WhatsApp audio message to the specified recipient. For group messages use the JID. If it errors due to ffmpeg not being installed, use send_file instead.
    
    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        media_path: The absolute path to the audio file to send (will be converted to Opus .ogg if it's not a .ogg file)
    
    Returns:
        A dictionary containing success status and a status message
    """
    # Validate input
    if not recipient:
        return {
            "success": False,
            "message": "Recipient must be provided"
        }
    
    if not media_path:
        return {
            "success": False,
            "message": "Media path must be provided"
        }
    
    if not os.path.isfile(media_path):
        return {
            "success": False,
            "message": f"Media file not found: {media_path}"
        }
    
    # No need to convert to opus here, as the bridge API handles this
    
    payload = {
        "recipient": recipient,
        "media_path": media_path,
        "is_audio": True
    }
    
    return make_api_request("send", "POST", payload)

@mcp.tool()
def download_media(message_id: str, chat_jid: str) -> Dict[str, Any]:
    """Download media from a WhatsApp message and get the local file path.
    
    Args:
        message_id: The ID of the message containing the media
        chat_jid: The JID of the chat containing the message
    
    Returns:
        A dictionary containing success status, a status message, and the file path if successful
    """
    payload = {
        "message_id": message_id,
        "chat_jid": chat_jid
    }
    
    return make_api_request("download", "POST", payload)

if __name__ == "__main__":
    # Initialize and run the server
    mcp.run(transport='stdio')