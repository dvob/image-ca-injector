import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.URL;

public class HttpGet {
    public static void main(String[] args) {
        if ( args.length < 1 ) {
            System.err.println("missing argument");
            System.exit(1);
        }
        try {
            System.out.format("GET %s\n", args[0]);
            // Specify the URL you want to send the GET request to
            URL url = new URL(args[0]);

            // Open a connection to the URL
            HttpURLConnection connection = (HttpURLConnection) url.openConnection();

            // Set the request method to GET
            connection.setRequestMethod("GET");

            // Get the response code from the server
            int responseCode = connection.getResponseCode();
            System.out.println("Response Code: " + responseCode);

            connection.getInputStream().transferTo(System.out);

            // Close the connection
            connection.disconnect();
        } catch (IOException e) {
            e.printStackTrace();
            System.exit(1);
        }
    }
}
